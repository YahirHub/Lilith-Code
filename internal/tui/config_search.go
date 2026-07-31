package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
	"github.com/lilith/li/internal/tui/uikit/textinput"

	"github.com/lilith/li/internal/websearch"
)

type searchConfigView string

const (
	searchViewList     searchConfigView = "list"
	searchViewProvider searchConfigView = "provider"
	searchViewKey      searchConfigView = "key"
	searchViewOrder    searchConfigView = "order"
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
		view:     searchViewList,
		focus:    "search-key",
		selected: websearch.Tavily,
		input:    input,
	}
	state.reload()
	if websearch.ValidProvider(state.settings.DefaultProvider) {
		state.selected = state.settings.DefaultProvider
	}
	state.ensureSelectedProvider()
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
	s.ensureSelectedProvider()
}

func (s *searchConfigState) ensureSelectedProvider() {
	if websearch.ValidProvider(s.selected) {
		return
	}
	if websearch.ValidProvider(s.settings.DefaultProvider) {
		s.selected = s.settings.DefaultProvider
		return
	}
	if len(websearch.ProviderIDs) > 0 {
		s.selected = websearch.ProviderIDs[0]
	}
}

func (s *searchConfigState) resetToList() {
	s.view = searchViewList
	s.input.Blur()
	s.busy = false
	s.message = ""
	s.danger = ""
	s.ensureSelectedProvider()
}

func (s *searchConfigState) inNestedView() bool {
	return s.view != searchViewList
}

func (s *searchConfigState) nestedSubtitle() string {
	switch s.view {
	case searchViewKey:
		return "Búsqueda / " + websearch.Labels[s.selected] + " / API key"
	case searchViewOrder:
		return "Búsqueda / Orden de respaldo"
	default:
		return "Búsqueda / " + websearch.Labels[s.selected]
	}
}

func (s *searchConfigState) providerIndex() int {
	for i, id := range websearch.ProviderIDs {
		if id == s.selected {
			return i
		}
	}
	return 0
}

func (s *searchConfigState) atListTop() bool {
	return s.view == searchViewList && s.providerIndex() == 0
}

func (s *searchConfigState) moveProvider(delta int) {
	if len(websearch.ProviderIDs) == 0 {
		return
	}
	idx := s.providerIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(websearch.ProviderIDs) {
		idx = len(websearch.ProviderIDs) - 1
	}
	s.selected = websearch.ProviderIDs[idx]
}

func (s *searchConfigState) detailFocusOrder() []string {
	return []string{
		"search-key", "search-test", "search-toggle", "search-default",
		"search-remove", "search-order", "search-test-all", "search-detail-back",
	}
}

func (s *searchConfigState) moveDetailFocus(delta int) {
	order := s.detailFocusOrder()
	idx := 0
	for i, id := range order {
		if id == s.focus {
			idx = i
			break
		}
	}
	next := idx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(order) {
		next = len(order) - 1
	}
	s.focus = order[next]
}

func (s *searchConfigState) openProvider(provider websearch.ProviderID) {
	if !websearch.ValidProvider(provider) {
		return
	}
	s.selected = provider
	s.view = searchViewProvider
	s.focus = "search-key"
	s.message = ""
	s.danger = ""
}

func (s *searchConfigState) handleKey(msg uikit.KeyMsg) (bool, uikit.Cmd) {
	if s.view == searchViewKey {
		if s.busy {
			if msg.String() == "esc" {
				s.view = searchViewProvider
				s.input.Blur()
				return true, nil
			}
			return true, nil
		}
		switch msg.String() {
		case "esc":
			s.view = searchViewProvider
			s.input.Blur()
			s.danger = ""
			return true, nil
		case "enter":
			return true, s.saveAndTestKeyCmd()
		}
		var cmd uikit.Cmd
		s.input, cmd = s.input.Update(msg)
		return true, cmd
	}

	if s.view == searchViewOrder {
		switch msg.String() {
		case "esc":
			s.view = searchViewProvider
			s.danger = ""
			return true, nil
		case "up", "k":
			s.moveOrderCursor(-1)
			return true, nil
		case "down", "j":
			s.moveOrderCursor(1)
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
				s.view = searchViewProvider
				s.message = "Orden de respaldo guardado."
				s.danger = ""
			}
			return true, nil
		}
		return true, nil
	}

	if s.view == searchViewProvider {
		switch msg.String() {
		case "esc":
			s.view = searchViewList
			s.message = ""
			s.danger = ""
			return true, nil
		case "up", "k":
			s.moveDetailFocus(-1)
			return true, nil
		case "down", "j":
			s.moveDetailFocus(1)
			return true, nil
		case "left", "right", "h", "l", "tab", "shift+tab":
			// Horizontal navigation belongs to this nested screen. It must never
			// leak back to the General/Búsqueda/Seguridad section picker.
			return true, nil
		case "enter", " ":
			return true, s.activate(s.focus)
		}
		return true, nil
	}

	// Provider list. Horizontal keys are intentionally consumed while this
	// list has focus so they cannot switch the top /config section.
	switch msg.String() {
	case "up", "k":
		s.moveProvider(-1)
		return true, nil
	case "down", "j":
		s.moveProvider(1)
		return true, nil
	case "left", "right", "h", "l", "tab", "shift+tab":
		return true, nil
	case "enter", " ":
		s.openProvider(s.selected)
		return true, nil
	}
	return false, nil
}

func (s *searchConfigState) activate(id string) uikit.Cmd {
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
	case "search-detail-back":
		s.view = searchViewList
		s.message = ""
		s.danger = ""
	}
	return nil
}

func (s *searchConfigState) handleHit(id string) (bool, uikit.Cmd) {
	if s.view == searchViewKey {
		switch id {
		case "search-key-input":
			return true, s.input.Focus()
		case "search-key-save":
			return true, s.saveAndTestKeyCmd()
		case "search-key-cancel":
			s.view = searchViewProvider
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
				s.view = searchViewProvider
				s.message = "Orden de respaldo guardado."
			}
			return true, nil
		}
		if id == "search-order-back" {
			s.view = searchViewProvider
			return true, nil
		}
		return false, nil
	}
	if s.view == searchViewProvider {
		for _, candidate := range s.detailFocusOrder() {
			if candidate == id {
				s.focus = id
				return true, s.activate(id)
			}
		}
		return false, nil
	}
	if strings.HasPrefix(id, "search-provider:") {
		provider := websearch.ProviderID(strings.TrimPrefix(id, "search-provider:"))
		if websearch.ValidProvider(provider) {
			s.openProvider(provider)
		}
		return true, nil
	}
	return false, nil
}

func (s *searchConfigState) saveAndTestKeyCmd() uikit.Cmd {
	provider := s.selected
	key := strings.TrimSpace(s.input.Value())
	if key == "" {
		s.danger = "La API key no puede estar vacía."
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() uikit.Msg {
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

func (s *searchConfigState) testProviderCmd(provider websearch.ProviderID) uikit.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() uikit.Msg {
		ok, message := websearch.TestProvider(context.Background(), s.ctx.ConfigDir, provider)
		if err := websearch.RecordTest(s.ctx.ConfigDir, provider, ok, message); err != nil {
			return searchTestMsg{provider: provider, ok: ok, message: message, err: err}
		}
		return searchTestMsg{provider: provider, ok: ok, message: message}
	}
}

func (s *searchConfigState) testAllCmd() uikit.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	s.danger = ""
	s.message = ""
	return func() uikit.Msg {
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
		s.view = searchViewProvider
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

func (s *searchConfigState) moveOrderCursor(delta int) {
	if len(s.order) == 0 {
		return
	}
	next := s.orderAt + delta
	if next < 0 {
		next = 0
	}
	if next >= len(s.order) {
		next = len(s.order) - 1
	}
	s.orderAt = next
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
		return "ACTIVO"
	case state.Configured:
		return "CONFIGURADO"
	default:
		return "SIN CONFIGURAR"
	}
}
