package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lilith/li/internal/rewind"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

type rewindRestoreMode string

const (
	rewindBoth         rewindRestoreMode = "both"
	rewindConversation rewindRestoreMode = "conversation"
	rewindCode         rewindRestoreMode = "code"
)

type rewindStage int

const (
	rewindPickPoint rewindStage = iota
	rewindConfirm
	rewindWorking
)

const rewindOperationTimeout = 10 * time.Minute

// RewindModel mirrors Claude Code's two-step selector: choose a previous user
// boundary, then choose whether to restore code, conversation, or both.
type RewindModel struct {
	ctx             *AppContext
	chat            *ChatModel
	points          []rewind.Meta
	cursor          int
	stage           rewindStage
	selected        rewind.Meta
	option          int
	err             string
	operationID     uint64
	operationCancel context.CancelFunc
}

func NewRewindScreen(ctx *AppContext, chat *ChatModel) *RewindModel {
	m := &RewindModel{ctx: ctx, chat: chat}
	if chat == nil || chat.rewindStore == nil || chat.sess == nil {
		m.err = "El historial de rewind no está disponible."
		return m
	}
	points, err := chat.rewindStore.List(chat.project, chat.sess.ID)
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.points = points
	if len(points) > 0 {
		m.cursor = len(points) - 1
	}
	return m
}

func (m *RewindModel) Init() uikit.Cmd { return uikit.WindowSize() }

func (m *RewindModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case uikit.WindowSizeMsg:
		m.ctx.Width, m.ctx.Height = v.Width, v.Height
		return m, nil
	case rewindOperationResultMsg:
		if v.operationID != m.operationID {
			// The user canceled this operation or started another one. Its worker
			// may still be unwinding, but it must never apply a stale rewind.
			return m, nil
		}
		if m.operationCancel != nil {
			m.operationCancel()
			m.operationCancel = nil
		}
		m.stage = rewindConfirm
		if v.err != nil {
			switch {
			case errors.Is(v.err, context.DeadlineExceeded):
				m.err = "La restauración excedió 10 minutos y fue cancelada. Si incluía código, revisa el workspace antes de intentarlo de nuevo."
			case errors.Is(v.err, context.Canceled):
				m.err = "Restauración cancelada. Si incluía código, revisa el workspace: una operación de archivos ya iniciada puede haber quedado parcial."
			default:
				m.err = v.err.Error()
			}
			return m, nil
		}
		m.chat.applyRewindPoint(v.point, v.mode)
		return m, switchToChat()
	case uikit.KeyMsg:
		return m.updateKey(v)
	}
	return m, nil
}

func (m *RewindModel) updateKey(key uikit.KeyMsg) (uikit.Model, uikit.Cmd) {
	switch m.stage {
	case rewindPickPoint:
		switch key.String() {
		case "esc", "q":
			return m, switchToChat()
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+n":
			if m.cursor < len(m.points)-1 {
				m.cursor++
			}
		case "home":
			m.cursor = 0
		case "end":
			if len(m.points) > 0 {
				m.cursor = len(m.points) - 1
			}
		case "enter":
			if len(m.points) == 0 {
				return m, nil
			}
			m.selected = m.points[m.cursor]
			m.stage = rewindConfirm
			m.err = ""
			if !m.selected.HasCode {
				m.option = 1 // conversation
			} else {
				m.option = 0 // both
			}
		}
	case rewindConfirm:
		switch key.String() {
		case "esc":
			m.stage = rewindPickPoint
			m.err = ""
		case "up", "left", "shift+tab":
			if m.option > 0 {
				m.option--
			}
		case "down", "right", "tab":
			if m.option < 3 {
				m.option++
			}
		case "enter":
			if m.option == 3 {
				m.stage = rewindPickPoint
				return m, nil
			}
			mode := []rewindRestoreMode{rewindBoth, rewindConversation, rewindCode}[m.option]
			if (mode == rewindBoth || mode == rewindCode) && !m.selected.HasCode {
				m.err = "Ese punto sólo contiene la conversación; no hubo una herramienta mutante que exigiera snapshot de archivos."
				return m, nil
			}
			return m, m.startRestore(mode)
		}
	case rewindWorking:
		switch key.String() {
		case "esc", "q":
			if m.operationCancel != nil {
				m.operationCancel()
				m.operationCancel = nil
			}
			// Invalidate the worker result before returning control to confirmation.
			// Even if an OS operation takes a moment to unwind, it can no longer
			// apply code/conversation after the user canceled.
			m.operationID++
			m.stage = rewindConfirm
			m.err = "Restauración cancelada. Si incluía código, revisa el workspace antes de volver a intentarlo."
		}
	}
	return m, nil
}

func (m *RewindModel) startRestore(mode rewindRestoreMode) uikit.Cmd {
	if m.chat == nil || m.chat.rewindStore == nil || m.chat.sess == nil {
		m.err = "El historial de rewind no está disponible."
		return nil
	}
	if m.operationCancel != nil {
		m.operationCancel()
	}
	m.operationID++
	operationID := m.operationID
	opCtx, cancel := context.WithTimeout(context.Background(), rewindOperationTimeout)
	m.operationCancel = cancel
	m.stage = rewindWorking
	m.err = ""
	store := m.chat.rewindStore
	project := m.chat.project
	sessionID := m.chat.sess.ID
	pointID := m.selected.ID
	// Capture the current state on the TUI goroutine. The async command may then
	// create a safety checkpoint without reading mutable ChatModel fields.
	safetyConversation := m.chat.snapshotSessionForRewind()
	return func() (msg uikit.Msg) {
		defer func() {
			if recovered := recover(); recovered != nil {
				msg = rewindOperationResultMsg{operationID: operationID, err: fmt.Errorf("panic durante rewind: %v", recovered)}
			}
		}()
		defer cancel()
		if err := opCtx.Err(); err != nil {
			return rewindOperationResultMsg{operationID: operationID, err: err}
		}

		// A safety point makes an accidental rewind reversible. Conversation-only
		// rewind does not modify files, so capturing the entire workspace here was
		// unnecessary and could leave the UI waiting on a large Git repository.
		// Code/both modes still preserve the current files before restoring them.
		if safetyConversation != nil {
			safety, safetyErr := store.CreateSafetyContext(opCtx, project, sessionID, "Estado antes de rewind", safetyConversation)
			if safetyErr != nil {
				return rewindOperationResultMsg{operationID: operationID, err: fmt.Errorf("crear punto de seguridad: %w", safetyErr)}
			}
			if mode == rewindBoth || mode == rewindCode {
				if _, captureErr := store.CaptureWorkspaceContext(opCtx, project, sessionID, safety.ID); captureErr != nil {
					return rewindOperationResultMsg{operationID: operationID, err: fmt.Errorf("crear punto de seguridad de código: %w", captureErr)}
				}
			}
		}
		point, err := store.Load(project, sessionID, pointID)
		if err != nil {
			return rewindOperationResultMsg{operationID: operationID, err: err}
		}
		if mode == rewindBoth || mode == rewindCode {
			if err := store.RestoreWorkspaceContext(opCtx, point); err != nil {
				return rewindOperationResultMsg{operationID: operationID, err: err}
			}
		}
		if err := opCtx.Err(); err != nil {
			return rewindOperationResultMsg{operationID: operationID, err: err}
		}
		return rewindOperationResultMsg{operationID: operationID, point: point, mode: mode}
	}
}

type rewindOperationResultMsg struct {
	operationID uint64
	point       *rewind.Point
	mode        rewindRestoreMode
	err         error
}

func (m *RewindModel) View() string {
	if m.stage == rewindPickPoint {
		return m.pickView()
	}
	return m.confirmView()
}

func (m *RewindModel) pickView() string {
	items := make([]viewportSelectorItem, 0, len(m.points))
	for _, point := range m.points {
		secondary := humanAgo(point.CreatedAt)
		if point.Kind == "safety" {
			secondary += " · punto de seguridad"
		}
		switch {
		case point.HasCode && point.PartialCode:
			secondary += " · código parcial"
		case point.HasCode:
			secondary += " · conversación + código"
		case point.CodeError != "":
			secondary += " · sólo conversación (snapshot falló)"
		default:
			secondary += " · sólo conversación"
		}
		items = append(items, viewportSelectorItem{Primary: compactRewindPrompt(point.Prompt), Secondary: secondary})
	}
	return renderViewportSelector(m.ctx.Styles, viewportSelectorSpec{
		Title:        "Rewind",
		Subtitle:     "Restaura al estado inmediatamente anterior a un mensaje del usuario.",
		Items:        items,
		Selected:     m.cursor,
		EmptyText:    "Todavía no hay puntos de rewind. Se crean al iniciar cada nuevo turno.",
		Footer:       "↑↓ navegar · Enter continuar · Esc cancelar",
		Error:        m.err,
		ScreenWidth:  m.ctx.Width,
		ScreenHeight: m.ctx.Height,
	})
}

func (m *RewindModel) confirmView() string {
	s := m.ctx.Styles
	w := m.ctx.Width
	if w <= 0 {
		w = 80
	}
	title := s.Title.Foreground(s.Theme.Primary).Render("Confirmar rewind")
	prompt := tuistyle.NewStyle().Border(tuistyle.RoundedBorder()).BorderForeground(s.Theme.Muted).Padding(0, 1).Width(maxInt(20, minInt(w-6, 88))).Render(compactRewindPrompt(m.selected.Prompt))
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\nVolver al punto anterior a este mensaje:\n")
	b.WriteString(prompt)
	b.WriteString("\n\n")
	options := []struct {
		label string
		desc  string
	}{
		{"Restaurar código y conversación", "Revierte archivos y recorta el chat; el mensaje seleccionado vuelve al editor."},
		{"Restaurar sólo conversación", "No toca archivos; recorta el chat y devuelve el mensaje al editor."},
		{"Restaurar sólo código", "Revierte archivos, pero conserva la conversación actual."},
		{"Cancelar", "Volver a la lista sin cambiar nada."},
	}
	for i, option := range options {
		prefix := "  "
		style := tuistyle.NewStyle().Foreground(s.Theme.Foreground)
		if i == m.option {
			prefix = "› "
			style = style.Foreground(s.Theme.Primary).Bold(true)
		}
		disabled := (i == 0 || i == 2) && !m.selected.HasCode
		if disabled {
			style = style.Foreground(s.Theme.Muted)
		}
		b.WriteString(style.Render(prefix + option.label))
		b.WriteString("\n")
		b.WriteString(s.Muted.Render("    " + option.desc))
		b.WriteString("\n\n")
	}
	if m.selected.PartialCode {
		b.WriteString(s.Warning.Render("Aviso: el snapshot de archivos es parcial; algunos archivos grandes/generados quedaron fuera."))
		b.WriteString("\n\n")
	}
	if m.selected.CodeError != "" {
		b.WriteString(s.Warning.Render("Código no disponible: " + m.selected.CodeError))
		b.WriteString("\n\n")
	}
	if m.err != "" {
		b.WriteString(s.Danger.Render("Error: " + m.err))
		b.WriteString("\n\n")
	}
	if m.stage == rewindWorking {
		b.WriteString(s.Muted.Render("Restaurando… · Esc cancelar"))
	} else {
		b.WriteString(s.Muted.Render("↑↓ elegir · Enter confirmar · Esc volver"))
	}
	return tuistyle.NewStyle().Padding(1, 2).Render(strings.TrimSpace(b.String()))
}

func compactRewindPrompt(prompt string) string {
	prompt = strings.TrimSpace(strings.ReplaceAll(prompt, "\r", ""))
	prompt = strings.Join(strings.Fields(prompt), " ")
	if prompt == "" {
		return "(mensaje sin texto)"
	}
	runes := []rune(prompt)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return prompt
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *ChatModel) applyRewindPoint(point *rewind.Point, mode rewindRestoreMode) {
	if m == nil || point == nil {
		return
	}
	restoredCode := mode == rewindBoth || mode == rewindCode
	restoredConversation := mode == rewindBoth || mode == rewindConversation
	if restoredConversation {
		snapshot := sessionCloneForRewind(point.Conversation)
		if snapshot == nil {
			m.AddError("El checkpoint no contiene una conversación válida.")
			return
		}
		// The source session remains the active timeline; only its contents move
		// back. Fork provenance is preserved when rewinding inside a fork.
		snapshot.ID = m.sess.ID
		snapshot.ProjectPath = m.project
		m.LoadSession(snapshot)
		if n := len(m.messages); n > 0 && m.messages[n-1].Kind == MsgSystem && strings.HasPrefix(m.messages[n-1].Content, "Sesión reanudada:") {
			m.messages = m.messages[:n-1]
		}
		// LoadSession intentionally preserves the editor for normal session
		// navigation. A rewind, however, owns the next input value: a user point
		// restores its prompt and an internal safety point must leave it empty.
		m.textarea.SetValue("")
		if point.Meta.Kind != "safety" {
			m.textarea.SetValue(point.Meta.Prompt)
			m.textarea.CursorEnd()
		}
		m.syncInputHeight()
	}
	what := "conversación"
	if restoredCode && restoredConversation {
		what = "código y conversación"
	} else if restoredCode {
		what = "código"
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: fmt.Sprintf("Rewind completado: %s restaurados al estado anterior a «%s». Se creó un punto de seguridad previo por si necesitas volver.", what, compactRewindPrompt(point.Meta.Prompt)), Time: time.Now()})
	m.clearActiveRewindPoint()
	m.persist()
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	} else {
		m.refreshTranscript(true)
	}
}

func sessionCloneForRewind(in *session.Session) *session.Session {
	return session.Clone(in)
}

var _ uikit.Model = (*RewindModel)(nil)
