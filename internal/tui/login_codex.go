package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/lilith/li/internal/tui/uikit"

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

func (m *CodexLoginModel) Init() uikit.Cmd {
	return startBrowserFlowCmd()
}

func startBrowserFlowCmd() uikit.Cmd {
	return func() uikit.Msg {
		flow, err := openaiprov.StartBrowserFlow()
		return codexBrowserStartedMsg{flow: flow, err: err}
	}
}

func waitBrowserCallbackCmd(ctx context.Context, flow *openaiprov.OAuthFlow) uikit.Cmd {
	return func() uikit.Msg {
		tok, err := flow.Wait(ctx)
		return codexTokensMsg{tokens: tok, err: err}
	}
}

func startDeviceFlowCmd() uikit.Cmd {
	return func() uikit.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := openaiprov.RequestDeviceCode(ctx)
		return codexDeviceStartedMsg{info: info, err: err}
	}
}

func pollDeviceCmd(ctx context.Context, info *openaiprov.DeviceCodeInfo) uikit.Cmd {
	return func() uikit.Msg {
		tok, err := openaiprov.PollDeviceCode(ctx, info)
		return codexTokensMsg{tokens: tok, err: err}
	}
}

func (m *CodexLoginModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
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
		return m, uikit.Tick(1200*time.Millisecond, func(time.Time) uikit.Msg {
			return switchScreenMsg{next: nil}
		})

	case codexClipboardMsg:
		if v.err != nil {
			m.notice = "No se pudo copiar automáticamente; selecciona el texto manualmente."
		} else {
			m.notice = "Copiado al portapapeles."
		}
		return m, nil

	case uikit.MouseMsg:
		e, ok := mouseLeftPress(v)
		if !ok {
			return m, nil
		}
		_, hits := m.layout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return m, nil
		}
		switch hit.id {
		case "copy-url":
			if m.flow != nil {
				return m, copyToClipboardCmd(m.flow.AuthURL)
			}
		case "device":
			m.cleanup()
			m.step = codexStepStarting
			m.notice = ""
			return m, startDeviceFlowCmd()
		case "copy-code":
			if m.device != nil {
				return m, copyToClipboardCmd(m.device.UserCode)
			}
		case "copy-device-url":
			if m.device != nil {
				return m, copyToClipboardCmd(m.device.VerificationURL)
			}
		case "retry":
			m.cleanup()
			m.err = ""
			m.step = codexStepStarting
			m.notice = ""
			return m, startBrowserFlowCmd()
		case "back":
			m.cleanup()
			return m, switchTo(NewOnboarding(m.ctx, m.ctx.FirstRun))
		}
		return m, nil

	case uikit.KeyMsg:
		switch v.String() {
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

func (m *CodexLoginModel) layout() (string, []settingsHit) {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	c := newSettingsCanvas(w)
	c.block(settingsHeader(s, "Suscripción ChatGPT / Codex", "Flujo OAuth con controles reutilizables y soporte de clic."))
	c.blank()

	switch m.step {
	case codexStepStarting:
		c.line(s.Subtitle.Render("Preparando el flujo OAuth…"))
		c.blank()
		c.block(settingsButtonGroup(s, w, settingsButtonSpec{ID: "back", Label: "Volver"}))

	case codexStepBrowserWait:
		c.line(s.Title.Render("Autoriza Lilith en tu navegador"))
		c.line(s.Subtitle.Render("Copia la URL, autoriza tu cuenta y vuelve a la terminal."))
		c.blank()
		if m.flow != nil {
			c.block(settingsCard(s, settingsCardSpec{
				ID:          "copy-url",
				Title:       "URL de autorización",
				Description: m.flow.AuthURL,
				Badge:       "OAUTH",
				Width:       w,
			}))
			c.blank()
		}
		c.block(settingsButtonGroup(s, w,
			settingsButtonSpec{ID: "copy-url", Label: "Copiar URL"},
			settingsButtonSpec{ID: "device", Label: "Código de dispositivo"},
			settingsButtonSpec{ID: "back", Label: "Cancelar", Danger: true},
		))

	case codexStepDevicePolling:
		c.line(s.Title.Render("Introduce este código en tu navegador"))
		c.blank()
		if m.device != nil {
			c.block(settingsCard(s, settingsCardSpec{
				ID:          "copy-code",
				Title:       m.device.UserCode,
				Description: m.device.VerificationURL,
				Badge:       "DISPOSITIVO",
				Width:       w,
			}))
			c.blank()
		}
		c.block(settingsButtonGroup(s, w,
			settingsButtonSpec{ID: "copy-code", Label: "Copiar código"},
			settingsButtonSpec{ID: "copy-device-url", Label: "Copiar URL"},
			settingsButtonSpec{ID: "back", Label: "Cancelar", Danger: true},
		))
		c.blank()
		c.line(s.Muted.Render("Esperando confirmación…"))

	case codexStepDone:
		c.line(s.Success.Render("✓ " + m.msg))

	case codexStepError:
		c.line(s.Warning.Render("Falló el inicio de sesión"))
		c.blank()
		c.line(s.Subtitle.Render(settingsWrapPlain(m.err, w)))
		c.blank()
		c.block(settingsButtonGroup(s, w,
			settingsButtonSpec{ID: "retry", Label: "Reintentar"},
			settingsButtonSpec{ID: "device", Label: "Código de dispositivo"},
			settingsButtonSpec{ID: "back", Label: "Volver"},
		))
	}
	if m.notice != "" {
		c.blank()
		c.line(s.Success.Render("· " + settingsWrapPlain(m.notice, w)))
	}
	c.blank()
	c.block(settingsFooter(s, "clic en botones · C copiar · D dispositivo · R reintentar · Esc volver"))
	return c.render(m.ctx.Width)
}

func (m *CodexLoginModel) View() string {
	view, _ := m.layout()
	return view
}

func copyToClipboardCmd(text string) uikit.Cmd {
	return func() uikit.Msg {
		payload := base64.StdEncoding.EncodeToString([]byte(text))
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", payload)
		return codexClipboardMsg{err: err}
	}
}
