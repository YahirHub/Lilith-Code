package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lilith/li/internal/providers/openai"
)

type inertScreen struct {
	updates int
}

func (s *inertScreen) Init() tea.Cmd { return nil }
func (s *inertScreen) View() string  { return "settings" }
func (s *inertScreen) Update(tea.Msg) (tea.Model, tea.Cmd) {
	s.updates++
	return s, nil
}

func TestRootKeepsChatStreamAliveWhileAnotherScreenIsVisible(t *testing.T) {
	chat := newInputTestChat(t)
	primeTestRequest(t, chat)
	visible := &inertScreen{}
	root := RootModel{ctx: chat.ctx, chat: chat, current: visible}
	chunks := make(chan openai.Chunk)

	next, cmd := root.Update(activeStreamMsg(chat, chatStreamMsg{
		ch:    chunks,
		delta: "respuesta que llegó dentro de /config",
	}))
	got := next.(RootModel)

	if got.current != visible {
		t.Fatal("un chunk de chat no debe sustituir la pantalla visible")
	}
	if visible.updates != 0 {
		t.Fatal("los mensajes del runtime de chat no deben enviarse al componente de settings")
	}
	if chat.assistantActive < 0 || chat.messages[chat.assistantActive].Content != "respuesta que llegó dentro de /config" {
		t.Fatalf("el chat persistente no procesó el chunk: %#v", chat.messages)
	}
	if cmd == nil {
		t.Fatal("el chunk debe programar el siguiente streamPump; perder este comando congelaría el turno")
	}
}
