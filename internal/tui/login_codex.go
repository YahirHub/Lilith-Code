package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/providers"
	openaiprov "github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/secrets"
)

// codexStep controla la fase del asistente de login OAuth.
type codexStep int

const (
	codexStepStarting codexStep = iota
	codexStepBrowserWait
	codexStepDeviceInfo
	codexStepDevicePolling
	codexStepDone
	codexStepError
)

// CodexLoginModel implementa el login OAuth con la suscripción ChatGPT/Codex.
type CodexLoginModel struct {
	ctx    *AppContext
	step   codexStep
	flow   *openaiprov.OAuthFlow
	device *openaiprov.DeviceCodeInfo
	err    string
	msg    string
	notice string
	cancel context.CancelFunc
}

func NewCodexLogin(ctx *AppContext) *CodexLoginModel {
	return &CodexLoginModel{ctx: ctx}
}

// -- Mensajes internos --------------------------------------------------------

type codexBrowserStartedMsg struct {
	flow *openaiprov.OAuthFlow
	err  error
}
type codexTokensMsg struct {
	tokens secrets.OAuthTokens
	err    error
}
type codexDeviceStartedMsg struct {
	info *openaiprov.DeviceCodeInfo
	err  error
}
type codexClipboardMsg struct{ err error }

func (m *CodexLoginModel) Init() tea.Cmd {
	return startBrowserFlowCmd()
}

func startBrowserFlowCmd() tea.Cmd {
	return func() tea.Msg {
		flow, err := openaiprov.StartBrowserFlow()
		return codexBrowserStartedMsg{flow: flow, err: err}
	}
}

func waitBrowserCallbackCmd(ctx context.Context, flow *openaiprov.OAuthFlow) tea.Cmd {
	return func() tea.Msg {
		tok, err := flow.Wait(ctx)
		return codexTokensMsg{tokens: tok, err: err}
	}
}

func startDeviceFlowCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := openaiprov.RequestDeviceCode(ctx)
		return codexDeviceStartedMsg{info: info, err: err}
	}
}

func pollDeviceCmd(ctx context.Context, info *openaiprov.DeviceCodeInfo) tea.Cmd {
	return func() tea.Msg {
		tok, err := openaiprov.PollDeviceCode(ctx, info)
		return codexTokensMsg{tokens: tok, err: err}
	}
}

func (m *CodexLoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case codexBrowserStartedMsg:
		if v.err != nil {
			m.step = codexStepError
			m.err = "No se pudo iniciar el callback local: " + v.err.Error() + "\nPulsa D para probar con código de dispositivo."
			return m, nil
		}
		m.flow = v.flow
		m.step = codexStepBrowserWait
		m.notice = ""
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		return m, waitBrowserCallbackCmd(ctx, v.flow)

	case codexDeviceStartedMsg:
		if v.err != nil {
			m.step = codexStepError
			m.err = v.err.Error()
			return m, nil
		}
		m.device = v.info
		m.step = codexStepDevicePolling
		m.notice = ""
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		return m, pollDeviceCmd(ctx, v.info)

	case codexTokensMsg:
		if v.err != nil {
			m.step = codexStepError
			m.err = v.err.Error()
			return m, nil
		}
		if err := persistCodexTokens(m.ctx, v.tokens); err != nil {
			m.step = codexStepError
			m.err = err.Error()
			return m, nil
		}
		m.step = codexStepDone
		m.msg = "Sesión ChatGPT conectada. Ya puedes elegir modelos Codex desde /models."
		return m, tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg {
			return switchScreenMsg{next: nil}
		})

	case codexClipboardMsg:
		if v.err != nil {
			m.notice = "No se pudo copiar automáticamente; selecciona el texto manualmente."
		} else {
			m.notice = "Copiado al portapapeles."
		}
		return m, nil

	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			m.cleanup()
			return m, tea.Quit
		case "esc":
			m.cleanup()
			return m, switchTo(NewOnboarding(m.ctx, m.ctx.FirstRun))
		case "d", "D":
			if m.step == codexStepBrowserWait || m.step == codexStepError {
				m.cleanup()
				m.step = codexStepStarting
				m.notice = ""
				return m, startDeviceFlowCmd()
			}
		case "r", "R":
			if m.step == codexStepError {
				m.err = ""
				m.step = codexStepStarting
				m.notice = ""
				return m, startBrowserFlowCmd()
			}
		case "c", "C":
			if m.step == codexStepBrowserWait && m.flow != nil {
				return m, copyToClipboardCmd(m.flow.AuthURL)
			}
			if m.step == codexStepDevicePolling && m.device != nil {
				return m, copyToClipboardCmd(m.device.UserCode)
			}
		case "u", "U":
			if m.step == codexStepDevicePolling && m.device != nil {
				return m, copyToClipboardCmd(m.device.VerificationURL)
			}
		}
	}
	return m, nil
}

func persistCodexTokens(ctx *AppContext, tok secrets.OAuthTokens) error {
	if err := openaiprov.SaveTokens(ctx.ConfigDir, tok); err != nil {
		return err
	}
	if err := providers.SetActive(ctx.ConfigDir, providers.ChatGPTCodexID, ""); err != nil {
		return err
	}
	return ctx.ReloadProviders()
}

func (m *CodexLoginModel) cleanup() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.flow != nil {
		m.flow.Close()
		m.flow = nil
	}
}

func (m *CodexLoginModel) View() string {
	s := m.ctx.Styles
	var lines []string
	lines = append(lines,
		s.Accent.Render("Suscripción ChatGPT / Codex"),
		"",
	)
	switch m.step {
	case codexStepStarting:
		lines = append(lines, s.Subtitle.Render("Preparando el flujo OAuth…"))
	case codexStepBrowserWait:
		lines = append(lines,
			s.Title.Render("Abre esta URL en tu navegador"),
			"",
			s.Subtitle.Render("Lilith no abre enlaces automáticamente."),
			s.Subtitle.Render("Copia la URL, autoriza ChatGPT y vuelve a la terminal."),
			"",
			s.Muted.Render(m.flow.AuthURL),
			"",
			buttonRow(s, "C Copiar URL", "D Código dispositivo", "Esc Cancelar"),
		)
	case codexStepDevicePolling:
		lines = append(lines,
			s.Title.Render("Introduce este código en tu navegador"),
			"",
			s.Accent.Render(m.device.UserCode),
			s.Subtitle.Render("Ve a "+m.device.VerificationURL),
			"",
			buttonRow(s, "C Copiar código", "U Copiar URL", "Esc Cancelar"),
			s.Muted.Render("Esperando confirmación…"),
		)
	case codexStepDone:
		lines = append(lines, s.Success.Render("✓ "+m.msg))
	case codexStepError:
		lines = append(lines,
			s.Warning.Render("Falló el inicio de sesión"),
			"",
			s.Subtitle.Render(m.err),
			"",
			s.Muted.Render("R Reintentar (navegador)   D Código de dispositivo   Esc Volver"),
		)
	}
	if m.notice != "" {
		lines = append(lines, "", s.Success.Render(m.notice))
	}
	return centered(m.ctx.Width, m.ctx.Height, strings.Join(lines, "\n"))
}

func buttonRow(s Styles, labels ...string) string {
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, s.Badge.Render(label))
	}
	return strings.Join(parts, "  ")
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		payload := base64.StdEncoding.EncodeToString([]byte(text))
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", payload)
		return codexClipboardMsg{err: err}
	}
}
