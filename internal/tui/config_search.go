package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/websearch"
)

type searchConfigView string

const (
	searchViewMain  searchConfigView = "main"
	searchViewKey   searchConfigView = "key"
	searchViewOrder searchConfigView = "order"
)

type searchConfigState struct {
	ctx      *AppContext
	settings websearch.Settings
	auth     websearch.Auth

	view     searchConfigView
	focus    string
	selected websearch.ProviderID
	input    textinput.Model
	busy     bool
	message  string
	danger   string

	order   []websearch.ProviderID
	orderAt int
}

type searchTestMsg struct {
	provider websearch.ProviderID
	ok       bool
	message  string
	err      error
}

type searchTestAllMsg struct {
	lines []string
	err   error
}

func newSearchConfigState(ctx *AppContext) *searchConfigState {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Pega la API key"
	input.CharLimit = 4096
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	state := &searchConfigState{
		ctx:      ctx,
		view:     searchViewMain,
		focus:    "search-provider:tavily",
		selected: websearch.Tavily,
		input:    input,
	}
	state.reload()
	return state
}

func (s *searchConfigState) reload() {
	settings, auth, err := websearch.Load(s.ctx.ConfigDir)
	if err != nil {
		s.danger = err.Error()
		return
	}
	s.settings = settings
	s.auth = auth
}

func (s *searchConfigState) mainFocusOrder() []string {
	ids := make([]string, 0, len(websearch.ProviderIDs)+8)
	for _, id := range websearch.ProviderIDs {
		ids = append(ids, "search-provider:"+string(id))
	}
	ids = append(ids,
		"search-key", "search-test", "search-toggle", "search-default",
		"search-remove", "search-order", "search-test-all", "back",
	)
	return ids
}

func (s *searchConfigState) moveFocus(delta int) {
	if s.view == searchViewOrder {
		if len(s.order) == 0 {
			return
		}
		s.orderAt = (s.orderAt + delta) % len(s.order)
		if s.orderAt < 0 {
			s.orderAt += len(s.order)
		}
		return
	}
	order := s.mainFocusOrder()
	idx := 0
	for i, id := range order {
		if id == s.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(order)
	if idx < 0 {
		idx += len(order)
	}
	s.focus = order[idx]
	if strings.HasPrefix(s.focus, "search-provider:") {
		id := websearch.ProviderID(strings.TrimPrefix(s.focus, "search-provider:"))
		if websearch.ValidProvider(id) {
			s.selected = id
		}
	}
}

func (s *searchConfigState) handleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if s.view == searchViewKey {
		if s.busy {
			if msg.String() == "esc" {
				s.view = searchViewMain
				s.input.Blur()
				return true, nil
			}
			return true, nil
		}
		switch msg.String() {
		case "esc":
			s.view = searchViewMain
			s.input.Blur()
			s.danger = ""
			return true, nil
		case "enter":
			return true, s.saveAndTestKeyCmd()
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return true, cmd
	}

	if s.view == searchViewOrder {
		switch msg.String() {
		case "esc":
			s.view = searchViewMain
			s.danger = ""
			return true, nil
		case "up", "k":
			s.moveFocus(-1)
			return true, nil
		case "down", "j":
			s.moveFocus(1)
			return true, nil
		case "left", "h":
			s.moveOrder(-1)
			return true, nil
		case "right", "l":
			s.moveOrder(1)
			return true, nil
		case "enter", " ":
			if err := websearch.SetFallbackOrder(s.ctx.ConfigDir, s.fullOrder()); err != nil {
				s.danger = err.Error()
			} else {
				s.reload()
				s.view = searchViewMain
				s.message = "Orden de respaldo guardado."
				s.danger = ""
			}
			return true, nil
		}
		return true, nil
	}

	// Tab/Shift+Tab remain reserved for /config section navigation.
	if msg.String() == "tab" || msg.String() == "shift+tab" {
		return false, nil
	}
	switch msg.String() {
	case "up", "k":
		s.moveFocus(-1)
		return true, nil
	case "down", "j":
		s.moveFocus(1)
		return true, nil
	case "enter", " ":
		return true, s.activate(s.focus)
	}
	return false, nil
}

func (s *searchConfigState) activate(id string) tea.Cmd {
	if strings.HasPrefix(id, "search-provider:") {
		provider := websearch.ProviderID(strings.TrimPrefix(id, "search-provider:"))
		if websearch.ValidProvider(provider) {
			s.selected = provider
			s.focus = id
		}
		return nil
	}
	provider := s.selected
	state := websearch.Resolve(provider, s.settings, s.auth)
	s.message = ""
	s.danger = ""
	switch id {
	case "search-key":
		s.view = searchViewKey
		s.input.SetValue("")
		return s.input.Focus()
	case "search-test":
		if !state.Configured {
			s.danger = "Configura una API key antes de probar este motor."
			return nil
		}
		return s.testProviderCmd(provider)
	case "search-toggle":
		target := !state.EnabledByUser
		if err := websearch.SetEnabled(s.ctx.ConfigDir, provider, target); err != nil {
			s.danger = err.Error()
			return nil
		}
		s.reload()
		if target {
			s.message = websearch.Labels[provider] + " habilitado."
		} else {
			s.message = websearch.Labels[provider] + " deshabilitado."
		}
	case "search-default":
		if err := websearch.SetDefault(s.ctx.ConfigDir, provider); err != nil {
			s.danger = err.Error()
			return nil
		}
		s.reload()
		s.message = websearch.Labels[provider] + " es ahora el motor predeterminado."
	case "search-remove":
		if err := websearch.RemoveAPIKey(s.ctx.ConfigDir, provider); err != nil {
			s.danger = err.Error()
			return nil
		}
		s.reload()
		s.message = "API key eliminada de " + websearch.Labels[provider] + "."
	case "search-order":
		s.enterOrder()
	case "search-test-all":
		return s.testAllCmd()
	}
	return nil
}

func (s *searchConfigState) handleHit(id string) (bool, tea.Cmd) {
	if s.view == searchViewKey {
		switch id {
		case "search-key-input":
			return true, s.input.Focus()
		case "search-key-save":
			return true, s.saveAndTestKeyCmd()
		case "search-key-cancel":
			s.view = searchViewMain
			s.input.Blur()
			return true, nil
		}
		return false, nil
	}
	if s.view == searchViewOrder {
		if strings.HasPrefix(id, "search-order:") {
			provider := websearch.ProviderID(strings.TrimPrefix(id, "search-order:"))
			for i, candidate := range s.order {
				if candidate == provider {
					s.orderAt = i
					break
				}
			}
			return true, nil
		}
		if id == "search-order-save" {
			if err := websearch.SetFallbackOrder(s.ctx.ConfigDir, s.fullOrder()); err != nil {
				s.danger = err.Error()
			} else {
				s.reload()
				s.view = searchViewMain
				s.message = "Orden de respaldo guardado."
			}
			return true, nil
		}
		if id == "search-order-back" {
			s.view = searchViewMain
			return true, nil
		}
		return false, nil
	}
	if strings.HasPrefix(id, "search-provider:") {
		provider := websearch.ProviderID(strings.TrimPrefix(id, "search-provider:"))
		if websearch.ValidProvider(provider) {
			s.selected = provider
			s.focus = id
		}
		return true, nil
	}
	for _, candidate := range s.mainFocusOrder() {
		if candidate == id && id != "back" {
			s.focus = id
			return true, s.activate(id)
		}
	}
	return false, nil
}

func (s *searchConfigState) saveAndTestKeyCmd() tea.Cmd {
	provider := s.selected
	key := strings.TrimSpace(s.input.Value())
	if key == "" {
		s.danger = "La API key no puede estar vacía."
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() tea.Msg {
		if err := websearch.SaveAPIKey(s.ctx.ConfigDir, provider, key); err != nil {
			return searchTestMsg{provider: provider, err: err}
		}
		ok, message := websearch.TestProvider(context.Background(), s.ctx.ConfigDir, provider)
		if err := websearch.RecordTest(s.ctx.ConfigDir, provider, ok, message); err != nil {
			return searchTestMsg{provider: provider, ok: ok, message: message, err: err}
		}
		return searchTestMsg{provider: provider, ok: ok, message: message}
	}
}

func (s *searchConfigState) testProviderCmd(provider websearch.ProviderID) tea.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() tea.Msg {
		ok, message := websearch.TestProvider(context.Background(), s.ctx.ConfigDir, provider)
		if err := websearch.RecordTest(s.ctx.ConfigDir, provider, ok, message); err != nil {
			return searchTestMsg{provider: provider, ok: ok, message: message, err: err}
		}
		return searchTestMsg{provider: provider, ok: ok, message: message}
	}
}

func (s *searchConfigState) testAllCmd() tea.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() tea.Msg {
		_, auth, err := websearch.Load(s.ctx.ConfigDir)
		if err != nil {
			return searchTestAllMsg{err: err}
		}
		lines := []string{}
		for _, provider := range websearch.ProviderIDs {
			if strings.TrimSpace(auth.APIKeys[provider]) == "" {
				lines = append(lines, websearch.Labels[provider]+": sin API key")
				continue
			}
			ok, message := websearch.TestProvider(context.Background(), s.ctx.ConfigDir, provider)
			if err := websearch.RecordTest(s.ctx.ConfigDir, provider, ok, message); err != nil {
				return searchTestAllMsg{lines: lines, err: err}
			}
			status := "ERROR"
			if ok {
				status = "OK"
			}
			lines = append(lines, fmt.Sprintf("%s: %s · %s", websearch.Labels[provider], status, message))
		}
		return searchTestAllMsg{lines: lines}
	}
}

func (s *searchConfigState) applyTest(msg searchTestMsg) {
	s.busy = false
	s.reload()
	if msg.err != nil {
		s.danger = msg.err.Error()
		return
	}
	if msg.ok {
		s.message = websearch.Labels[msg.provider] + ": conexión validada."
		s.danger = ""
	} else {
		s.danger = websearch.Labels[msg.provider] + ": " + msg.message
	}
	if s.view == searchViewKey {
		s.view = searchViewMain
		s.input.Blur()
		s.input.SetValue("")
	}
}

func (s *searchConfigState) applyTestAll(msg searchTestAllMsg) {
	s.busy = false
	s.reload()
	if msg.err != nil {
		s.danger = msg.err.Error()
		return
	}
	s.message = strings.Join(msg.lines, "\n")
}

func (s *searchConfigState) enterOrder() {
	available := websearch.AvailableOrder(s.settings, s.auth)
	if len(available) < 2 {
		s.danger = "Valida al menos dos motores para ordenar respaldos."
		return
	}
	defaultID := s.settings.DefaultProvider
	if !websearch.Resolve(defaultID, s.settings, s.auth).Available {
		defaultID = available[0]
	}
	order := []websearch.ProviderID{}
	for _, id := range available {
		if id != defaultID {
			order = append(order, id)
		}
	}
	s.order = order
	s.orderAt = 0
	s.view = searchViewOrder
	s.danger = ""
}

func (s *searchConfigState) moveOrder(delta int) {
	if len(s.order) < 2 || s.orderAt < 0 || s.orderAt >= len(s.order) {
		return
	}
	next := s.orderAt + delta
	if next < 0 || next >= len(s.order) {
		return
	}
	s.order[s.orderAt], s.order[next] = s.order[next], s.order[s.orderAt]
	s.orderAt = next
}

func (s *searchConfigState) fullOrder() []websearch.ProviderID {
	defaultID := s.settings.DefaultProvider
	if !websearch.Resolve(defaultID, s.settings, s.auth).Available {
		available := websearch.AvailableOrder(s.settings, s.auth)
		if len(available) > 0 {
			defaultID = available[0]
		}
	}
	out := []websearch.ProviderID{}
	if defaultID != "" {
		out = append(out, defaultID)
	}
	out = append(out, s.order...)
	return out
}

func (s *searchConfigState) stateLabel(id websearch.ProviderID) string {
	state := websearch.Resolve(id, s.settings, s.auth)
	switch {
	case state.Available:
		return "VALIDADO"
	case state.Configured && !state.EnabledByUser:
		return "OFF"
	case state.Configured && state.LastTest != nil && !state.LastTest.OK:
		return "ERROR"
	case state.Configured:
		return "PENDIENTE"
	default:
		return "SIN TOKEN"
	}
}
