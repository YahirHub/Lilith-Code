package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/lilith/li/internal/interaction"
	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/lilith/li/internal/tui/uikit/textinput"
)

type permissionHit struct {
	row      int
	decision interaction.ApprovalDecision
}

type secretPromptState struct {
	input      textinput.Model
	first      string
	confirming bool
	errorText  string
}

func (m *ChatModel) hasPendingPermission() bool {
	return m != nil && m.permissionRequest != nil
}

func (m *ChatModel) openPermission(req *interaction.Request) uikit.Cmd {
	if req == nil {
		return nil
	}
	m.permissionRequest = req
	m.permissionSelected = 0
	m.secretPrompt = nil
	var focusCmd uikit.Cmd
	if req.Kind == interaction.Secret {
		in := textinput.New()
		in.Prompt = "❯ "
		in.Placeholder = "Escribe aquí…"
		in.CharLimit = 4096
		in.Width = maxInt(18, chatUsableWidth(m.ctx.Width)-8)
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
		in.PromptStyle = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Primary).Bold(true)
		in.TextStyle = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Foreground)
		in.PlaceholderStyle = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Muted)
		in.Cursor.Style = tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Success)
		focusCmd = in.Focus()
		m.secretPrompt = &secretPromptState{input: in}
	}
	m.returnToInteractionBottom()
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return uikit.Batch(focusCmd, m.chatMouseModeCmd())
}

func (m *ChatModel) resolvePermissionResult(result interaction.Result) uikit.Cmd {
	req := m.permissionRequest
	if m.secretPrompt != nil {
		m.secretPrompt.input.SetValue("")
		m.secretPrompt.first = ""
		m.secretPrompt.errorText = ""
	}
	m.secretPrompt = nil
	m.permissionRequest = nil
	m.permissionSelected = 0
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	if req == nil {
		return m.chatMouseModeCmd()
	}
	return uikit.Batch(
		func() uikit.Msg {
			return interactionResolvedMsg{request: req, result: result}
		},
		m.chatMouseModeCmd(),
	)
}

func (m *ChatModel) resolvePermission(decision interaction.ApprovalDecision) uikit.Cmd {
	approved := decision != interaction.ApprovalDeny
	result := interaction.Result{Approved: approved, Decision: decision}
	if !approved {
		result.Canceled = true
	}
	return m.resolvePermissionResult(result)
}

func secretInputLabel(req *interaction.Request, confirming bool) string {
	if req == nil {
		return "Contraseña"
	}
	label := "Contraseña"
	switch req.SecretKind {
	case interaction.SecretVaultMaster:
		label = "Contraseña maestra de la bóveda SSH"
	case interaction.SecretRemotePassword:
		label = "Contraseña del servidor remoto"
	case interaction.SecretSudoPassword:
		label = "Contraseña sudo del servidor remoto"
	case interaction.SecretKeyPassphrase:
		label = "Passphrase de la clave privada SSH"
	case interaction.SecretBrowserPassword:
		label = "Contraseña o secreto del sitio web"
	default:
		if strings.TrimSpace(req.Title) != "" {
			label = strings.TrimSpace(req.Title)
		}
	}
	if confirming {
		return "Confirmar " + strings.ToLower(label[:1]) + label[1:]
	}
	return label
}

type permissionOption struct {
	label    string
	decision interaction.ApprovalDecision
}

func permissionOptions(req *interaction.Request) []permissionOption {
	options := []permissionOption{{label: "Permitir una vez", decision: interaction.ApprovalOnce}}
	if req != nil && req.AllowScope {
		options = append(options,
			permissionOption{label: "Permitir en esta sesión", decision: interaction.ApprovalSession},
			permissionOption{label: "Permitir siempre en este proyecto", decision: interaction.ApprovalProject},
		)
	}
	return append(options, permissionOption{label: "Denegar", decision: interaction.ApprovalDeny})
}

func compactInteractionWrap(text string, width, maxLines int) []string {
	if width < 10 {
		width = 10
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, maxLines)
	line := words[0]
	for _, word := range words[1:] {
		if len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = word
			if maxLines > 0 && len(lines) == maxLines-1 {
				break
			}
		} else {
			line += " " + word
		}
	}
	if maxLines <= 0 || len(lines) < maxLines {
		lines = append(lines, line)
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func (m *ChatModel) permissionDockLayout(w int) (string, []permissionHit) {
	req := m.permissionRequest
	if req == nil {
		return "", nil
	}
	if w <= 0 {
		w = 80
	}
	outerWidth := chatUsableWidth(w)
	if outerWidth < 28 {
		outerWidth = 28
	}
	contentWidth := outerWidth - 4
	if contentWidth < 20 {
		contentWidth = 20
	}
	theme := m.ctx.Styles.Theme
	accent := tuistyle.NewStyle().Foreground(theme.Primary).Bold(true)
	strong := tuistyle.NewStyle().Foreground(theme.Foreground).Bold(true)
	plain := tuistyle.NewStyle().Foreground(theme.Foreground)
	muted := m.ctx.Styles.Muted
	danger := m.ctx.Styles.Danger

	icon := "◆"
	if req.Kind == interaction.Secret {
		icon = "🔐"
	}
	lines := []string{accent.Render(icon) + strong.Render("  "+strings.TrimSpace(req.Title))}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		if req.Kind == interaction.Secret {
			message = "Esta credencial se solicita localmente y no se enviará al modelo."
		} else {
			message = "Esta acción necesita tu aprobación local."
		}
	}
	maxMessageLines := 5
	if m.ctx.Height > 0 && m.ctx.Height <= 18 {
		maxMessageLines = 3
	}
	for _, line := range compactInteractionWrap(message, contentWidth-2, maxMessageLines) {
		lines = append(lines, "  "+plain.Render(line))
	}
	lines = append(lines, "")

	if req.Kind == interaction.Secret && m.secretPrompt != nil {
		state := m.secretPrompt
		state.input.Width = maxInt(16, contentWidth-4)
		lines = append(lines, "  "+strong.Render(secretInputLabel(req, state.confirming)))
		lines = append(lines, "  "+state.input.View())
		if state.errorText != "" {
			lines = append(lines, "", "  "+danger.Render(state.errorText))
		}
		lines = append(lines, "", muted.Render("  Entrada local protegida · no se envía al modelo ni se guarda en el historial"))
		lines = append(lines, muted.Render("  Enter confirmar · Esc cancelar"))
		card := tuistyle.NewStyle().Width(contentWidth).Padding(0, 1).Border(tuistyle.RoundedBorder()).BorderForeground(theme.Primary).Render(strings.Join(lines, "\n"))
		return card, nil
	}

	options := permissionOptions(req)
	if m.permissionSelected < 0 || m.permissionSelected >= len(options) {
		m.permissionSelected = 0
	}
	hits := make([]permissionHit, 0, len(options))
	for index, option := range options {
		row := len(lines)
		prefix := "  "
		optionStyle := plain
		if option.decision == interaction.ApprovalDeny {
			optionStyle = danger
		}
		if index == m.permissionSelected {
			prefix = "› "
			if option.decision == interaction.ApprovalDeny {
				optionStyle = danger.Bold(true)
			} else {
				optionStyle = accent
			}
		}
		lines = append(lines, prefix+optionStyle.Render(option.label))
		hits = append(hits, permissionHit{row: row + 1, decision: option.decision})
	}
	if req.AllowScope {
		lines = append(lines, muted.Render("  Sesión/proyecto recuerdan esta acción para este destino SSH."))
	}
	lines = append(lines, muted.Render("  ←→/↑↓ elegir · Enter confirmar · Esc denegar"))
	card := tuistyle.NewStyle().Width(contentWidth).Padding(0, 1).Border(tuistyle.RoundedBorder()).BorderForeground(theme.Primary).Render(strings.Join(lines, "\n"))
	return card, hits
}

func (m *ChatModel) permissionDockView(w int) string {
	view, _ := m.permissionDockLayout(w)
	return view
}

func (m *ChatModel) handleSecretPromptKey(v uikit.KeyMsg) (bool, uikit.Cmd) {
	req := m.permissionRequest
	state := m.secretPrompt
	if req == nil || req.Kind != interaction.Secret || state == nil {
		return false, nil
	}
	switch v.String() {
	case "esc", "ctrl+c":
		return true, m.resolvePermissionResult(interaction.Result{Canceled: true})
	case "enter":
		value := state.input.Value()
		if utf8.RuneCountInString(value) < req.MinLength {
			state.errorText = "La contraseña debe tener al menos " + intText(req.MinLength) + " caracteres."
			return true, nil
		}
		if req.Confirm && !state.confirming {
			state.first = value
			state.input.SetValue("")
			state.input.Placeholder = "Repite la contraseña…"
			state.confirming = true
			state.errorText = ""
			return true, nil
		}
		if state.confirming && value != state.first {
			state.input.SetValue("")
			state.errorText = "Las contraseñas no coinciden. Repite la confirmación."
			return true, nil
		}
		if state.confirming {
			value = state.first
		}
		return true, m.resolvePermissionResult(interaction.Result{Value: value, Approved: true})
	default:
		state.errorText = ""
		var cmd uikit.Cmd
		state.input, cmd = state.input.Update(v)
		return true, cmd
	}
}

func (m *ChatModel) handlePermissionKey(v uikit.KeyMsg) (bool, uikit.Cmd) {
	if !m.hasPendingPermission() {
		return false, nil
	}
	if m.userScrolled {
		m.returnToInteractionBottom()
	}
	if m.permissionRequest.Kind == interaction.Secret {
		return m.handleSecretPromptKey(v)
	}
	switch v.String() {
	case "left", "up", "h", "k", "shift+tab":
		if m.permissionSelected > 0 {
			m.permissionSelected--
		}
		return true, nil
	case "right", "down", "l", "j", "tab":
		if m.permissionSelected < len(permissionOptions(m.permissionRequest))-1 {
			m.permissionSelected++
		}
		return true, nil
	case "enter", " ":
		options := permissionOptions(m.permissionRequest)
		if m.permissionSelected < 0 || m.permissionSelected >= len(options) {
			m.permissionSelected = 0
		}
		return true, m.resolvePermission(options[m.permissionSelected].decision)
	case "esc", "n", "q":
		return true, m.resolvePermission(interaction.ApprovalDeny)
	case "y", "s":
		return true, m.resolvePermission(interaction.ApprovalOnce)
	default:
		return true, nil
	}
}

func (m *ChatModel) permissionChromeY() int {
	w, h := m.ctx.Width, m.ctx.Height
	if w <= 0 {
		w = 80
	}
	used, maxCtx := m.contextUsage()
	return m.viewportHeightForFrame(w, h, used, maxCtx)
}

func (m *ChatModel) handlePermissionMouse(v uikit.MouseMsg) (bool, uikit.Cmd) {
	if !m.hasPendingPermission() {
		return false, nil
	}
	// Secret widgets deliberately accept keyboard input only. This prevents an
	// accidental click from submitting or exposing a sensitive value.
	if m.permissionRequest.Kind == interaction.Secret {
		return true, nil
	}
	e, ok := mouseLeftPress(v)
	if !ok {
		return false, nil
	}
	_, hits := m.permissionDockLayout(m.ctx.Width)
	rel := e.Y - m.permissionChromeY()
	for _, hit := range hits {
		if hit.row != rel {
			continue
		}
		for index, option := range permissionOptions(m.permissionRequest) {
			if option.decision == hit.decision {
				m.permissionSelected = index
				break
			}
		}
		return true, m.resolvePermission(hit.decision)
	}
	return true, nil
}

func intText(v int) string {
	if v <= 0 {
		return "0"
	}
	const digits = "0123456789"
	out := ""
	for v > 0 {
		out = string(digits[v%10]) + out
		v /= 10
	}
	return out
}
