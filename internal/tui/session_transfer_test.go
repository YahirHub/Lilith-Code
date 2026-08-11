package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
)

func TestPortableChatPathUsesCurrentDirectoryAndJSONLExtension(t *testing.T) {
	base := t.TempDir()
	got, err := portableChatPath(base, "mi chat")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	want := filepath.Join(base, "mi chat.jsonl")
	if got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
	if _, err := portableChatPath(base, "chat.json"); err == nil {
		t.Fatal("una extensión distinta de .jsonl debe rechazarse")
	}
}

func TestExportImportConversationRebindsToCurrentDirectory(t *testing.T) {
	m := newInputTestChat(t)
	cwd := t.TempDir()
	originalGetCwd := getCwd
	getCwd = func() (string, error) { return cwd, nil }
	t.Cleanup(func() { getCwd = originalGetCwd })

	sourceID := m.sess.ID
	m.sess.Title = "Chat portable"
	m.history = []openai.Message{
		{Role: "user", Content: "continúa la implementación"},
		{Role: "assistant", Content: "avance guardado"},
	}
	m.messages = []ChatMessage{
		{Kind: MsgUser, Content: "continúa la implementación"},
		{Kind: MsgAssistant, Content: "avance guardado"},
	}

	message, err := m.exportConversation("traspaso.jsonl")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(message, "Conversación exportada") {
		t.Fatalf("mensaje export inesperado: %q", message)
	}
	path := filepath.Join(cwd, "traspaso.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leer export: %v", err)
	}
	if strings.Contains(string(raw), "projectPath") {
		t.Fatalf("el JSONL no debe transportar projectPath:\n%s", raw)
	}

	// Simula que el chat activo antes de importar pertenece a otro proyecto.
	m.project = filepath.Join(t.TempDir(), "otro")
	m.sess = session.New(m.project)
	m.history = []openai.Message{{Role: "user", Content: "otro chat"}}
	m.messages = []ChatMessage{{Kind: MsgUser, Content: "otro chat"}}

	message, err = m.importConversation("traspaso.jsonl")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(message, "vinculada al directorio actual") || strings.Contains(message, filepath.Clean(cwd)) {
		t.Fatalf("mensaje import inesperado: %q", message)
	}
	if m.project != filepath.Clean(cwd) || m.sess.ProjectPath != filepath.Clean(cwd) {
		t.Fatalf("import no quedó anclado al cwd: project=%q session=%q", m.project, m.sess.ProjectPath)
	}
	if m.sess.ID == sourceID {
		t.Fatalf("la sesión importada debe tener ID local nuevo: %q", m.sess.ID)
	}
	if len(m.history) != 2 || m.history[0].Content != "continúa la implementación" {
		t.Fatalf("historial no restaurado: %+v", m.history)
	}
	if _, err := m.store.Load(cwd, m.sess.ID); err != nil {
		t.Fatalf("la sesión importada debe persistirse en el proyecto actual: %v", err)
	}
}

func TestExportImportRejectDuringActiveTurn(t *testing.T) {
	m := newInputTestChat(t)
	primeTestRequest(t, m)
	if _, err := m.exportConversation("chat.jsonl"); err == nil || !strings.Contains(err.Error(), "turno activo") {
		t.Fatalf("export durante turno debía rechazarse, err=%v", err)
	}
	if _, err := m.importConversation("chat.jsonl"); err == nil || !strings.Contains(err.Error(), "turno activo") {
		t.Fatalf("import durante turno debía rechazarse, err=%v", err)
	}
}
