package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/tools"
	"github.com/lilith/li/internal/tui/uikit"
)

type rewindTurnState struct {
	mu            sync.Mutex
	pointID       string
	project       string
	sessionID     string
	codeCaptured  bool
	codeAttempted bool
	codeError     string
	externalOps   int
}

func (m *ChatModel) activeRewindTurn() *rewindTurnState {
	if m.rewindTurn == nil {
		m.rewindTurn = &rewindTurnState{}
	}
	return m.rewindTurn
}

// snapshotSessionForRewind captures the exact stable conversation state before
// a new user action. The provider history and transcript are detached from the
// live model so later streaming/tool updates cannot mutate the checkpoint.
func (m *ChatModel) beginRewindExternalOperation() {
	state := m.activeRewindTurn()
	state.mu.Lock()
	state.externalOps++
	state.mu.Unlock()
}

func (m *ChatModel) endRewindExternalOperation() {
	state := m.activeRewindTurn()
	state.mu.Lock()
	if state.externalOps > 0 {
		state.externalOps--
	}
	state.mu.Unlock()
}

func (m *ChatModel) rewindSessionBusy() bool {
	if m == nil {
		return false
	}
	if m.activeTurnID != 0 {
		return true
	}
	state := m.activeRewindTurn()
	state.mu.Lock()
	externalBusy := state.externalOps > 0
	state.mu.Unlock()
	if externalBusy {
		return true
	}
	for _, panel := range m.agentPanels {
		if panel != nil && panel.Background && (panel.Status == "" || panel.Status == "running") {
			return true
		}
	}
	return false
}

func (m *ChatModel) snapshotSessionForRewind() *session.Session {
	if m == nil {
		return nil
	}
	base := session.Clone(m.sess)
	if base == nil {
		base = session.New(m.project)
	}
	base.ProjectPath = filepath.Clean(m.project)
	base.Messages = cloneHistoryMessages(m.history)
	base.Transcript = m.snapshotTranscriptRange(0, len(m.messages))
	base.Todo = m.todoStatePointer()
	base.Plan = m.planStatePointer()
	base.Goal = m.goalStatePointer()
	base.Revision = m.persistRevision
	base.Live = nil
	return base
}

func (m *ChatModel) beginRewindPoint(prompt string) {
	if m == nil || m.rewindStore == nil || m.sess == nil {
		return
	}
	if strings.TrimSpace(prompt) == "" {
		return
	}
	snapshot := m.snapshotSessionForRewind()
	meta, err := m.rewindStore.Create(m.project, m.sess.ID, prompt, snapshot)
	state := m.activeRewindTurn()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.pointID = ""
	state.project = ""
	state.sessionID = ""
	state.codeCaptured = false
	state.codeAttempted = false
	state.codeError = ""
	if err != nil {
		state.codeAttempted = true
		state.codeError = err.Error()
		m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: "Aviso: no se pudo crear el checkpoint de conversación para /rewind: " + err.Error(), Time: time.Now()})
		return
	}
	state.pointID = meta.ID
	state.project = filepath.Clean(m.project)
	state.sessionID = m.sess.ID
}

func (m *ChatModel) ensureActiveRewindWorkspace() error {
	if m == nil || m.rewindStore == nil || m.sess == nil {
		return nil
	}
	state := m.activeRewindTurn()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.codeCaptured {
		return nil
	}
	if state.codeAttempted {
		if state.codeError != "" {
			return errors.New(state.codeError)
		}
		return errors.New("el snapshot de código ya fue intentado y no quedó disponible")
	}
	if state.pointID == "" || state.project == "" || state.sessionID == "" {
		return errors.New("no existe un checkpoint de conversación activo")
	}
	state.codeAttempted = true
	_, err := m.rewindStore.CaptureWorkspace(state.project, state.sessionID, state.pointID)
	if err != nil {
		state.codeError = err.Error()
		return err
	}
	state.codeCaptured = true
	state.codeError = ""
	return nil
}

func (m *ChatModel) rewindToolMayMutate(name string) bool {
	if strings.EqualFold(name, "run_terminal_command") || strings.EqualFold(name, "Agent") || strings.EqualFold(name, "Task") {
		return true
	}
	if def, ok := tools.Get(name); ok {
		return def.Mutating
	}
	// Dynamic MCP tools do not pass through the static registry. Treat every
	// MCP tool as mutating unless its server explicitly advertised readOnlyHint.
	// This mirrors Plan mode's fail-closed policy and ensures third-party tools
	// cannot change files before the checkpoint exists.
	if m != nil && m.mcpRuntime != nil && m.mcpRuntime.Has(name) {
		return !m.mcpRuntime.IsReadOnly(name)
	}
	return false
}

func (m *ChatModel) clearActiveRewindPoint() {
	state := m.activeRewindTurn()
	state.mu.Lock()
	state.pointID = ""
	state.project = ""
	state.sessionID = ""
	state.codeCaptured = false
	state.codeAttempted = false
	state.codeError = ""
	state.mu.Unlock()
}

func goalCommandNeedsCheckpoint(args string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(args)))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "status", "show", "clear", "remove", "delete", "pause", "pausar", "complete", "completed", "completar":
		return false
	default:
		return true
	}
}

func (m *ChatModel) forkSessionValidationError() error {
	if m == nil || m.rewindStore == nil || m.sess == nil {
		return errors.New("el historial de fork no está disponible")
	}
	if m.rewindSessionBusy() {
		return errors.New("/fork sólo puede ejecutarse cuando el agente y sus tareas en background están inactivos")
	}
	conversation := m.snapshotSessionForRewind()
	if conversation == nil || (len(conversation.Messages) == 0 && len(conversation.Transcript) == 0) {
		return errors.New("/fork requiere una conversación con al menos un mensaje")
	}
	return nil
}

func (m *ChatModel) runForkSessionCommand(title string) uikit.Cmd {
	if err := m.forkSessionValidationError(); err != nil {
		if m != nil {
			m.AddError(err.Error() + ".")
		}
		return nil
	}
	return switchTo(NewForkDestinationScreen(m.ctx, m, title))
}

func (m *ChatModel) startForkSessionAt(title, destination string) uikit.Cmd {
	if err := m.forkSessionValidationError(); err != nil {
		return func() uikit.Msg { return forkSessionResultMsg{err: err} }
	}
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return func() uikit.Msg { return forkSessionResultMsg{err: errors.New("ruta de destino del fork vacía")} }
	}
	project := m.project
	sessionID := m.sess.ID
	store := m.rewindStore
	sessionStore := m.store
	conversation := m.snapshotSessionForRewind()
	title = strings.TrimSpace(title)
	destination = filepath.Clean(destination)
	return func() uikit.Msg {
		meta, err := store.CreateSafety(project, sessionID, "Estado usado para crear el fork", conversation)
		if err != nil {
			return forkSessionResultMsg{err: err}
		}
		if _, err := store.CaptureWorkspace(project, sessionID, meta.ID); err != nil {
			return forkSessionResultMsg{err: fmt.Errorf("capturar archivos para el fork: %w", err)}
		}
		point, err := store.Load(project, sessionID, meta.ID)
		if err != nil {
			return forkSessionResultMsg{err: err}
		}
		workspace, err := store.ForkWorkspace(point, destination)
		if err != nil {
			return forkSessionResultMsg{err: err}
		}
		forked := session.Fork(conversation, workspace.ProjectPath, title, meta.ID)
		if err := sessionStore.Save(forked); err != nil {
			return forkSessionResultMsg{err: fmt.Errorf("guardar conversación fork: %w", err), orphanPath: workspace.Root}
		}
		return forkSessionResultMsg{sess: forked, root: workspace.Root, project: workspace.ProjectPath, kind: workspace.Kind}
	}
}

type forkSessionResultMsg struct {
	sess       *session.Session
	root       string
	project    string
	kind       string
	orphanPath string
	err        error
}

func (m *ChatModel) applyForkResult(result forkSessionResultMsg) uikit.Cmd {
	if result.err != nil {
		message := "No se pudo crear el fork: " + result.err.Error()
		if result.orphanPath != "" {
			message += "\nLa copia de archivos quedó en: " + result.orphanPath
		}
		m.AddError(message)
		return nil
	}
	if result.sess == nil || strings.TrimSpace(result.project) == "" {
		m.AddError("El fork terminó sin una sesión o ruta válida.")
		return nil
	}
	if err := os.Chdir(result.project); err != nil {
		m.AddError("El fork fue creado, pero no se pudo cambiar al nuevo proyecto: " + err.Error() + "\nRuta: " + result.project)
		return nil
	}
	if m.mcpRuntime != nil {
		_ = m.mcpRuntime.Close()
		m.mcpRuntime = nil
		m.mcpSignature = ""
	}
	m.project = filepath.Clean(result.project)
	m.agentCatalog = nil
	m.LoadSession(result.sess)
	m.clearActiveRewindPoint()
	m.messages = append(m.messages, ChatMessage{
		Kind:    MsgSystem,
		Content: "Fork creado y activado.\nProyecto: " + result.project + "\nCopia raíz: " + result.root + "\nLa conversación y los archivos originales permanecen intactos.",
		Time:    time.Now(),
	})
	m.persist()
	m.refreshTranscript(true)
	return uikit.Batch(m.connectMCP(), m.chatMouseModeCmd())
}
