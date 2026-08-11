package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	litodo "github.com/lilith/li/internal/todo"
)

func TestPortableJSONLRoundTripBindsCurrentProjectWithoutSourcePath(t *testing.T) {
	sourceProject := filepath.Join(t.TempDir(), "equipo-origen", "proyecto")
	destinationProject := filepath.Join(t.TempDir(), "equipo-destino", "proyecto")
	if err := os.MkdirAll(destinationProject, 0o755); err != nil {
		t.Fatal(err)
	}
	source := New(sourceProject)
	source.Title = "Continuar migración"
	source.Messages = []openai.Message{
		{Role: "user", Content: "Continúa con el parser"},
		{Role: "assistant", Content: "Ya quedó la primera etapa."},
	}
	source.Transcript = []TranscriptEntry{{Kind: "user", Content: "Continúa con el parser"}, {Kind: "assistant", Content: "Ya quedó la primera etapa."}}
	source.Compactions = []CompactionRecord{{ID: "compact-1", CreatedAt: time.Now(), Summary: "avance previo", ArchivedMessages: []openai.Message{{Role: "user", Content: "contexto anterior"}}}}
	source.Todo = &litodo.State{SchemaVersion: litodo.SchemaVersion, Revision: 4, Tasks: []litodo.Task{{Key: "verify", Subject: "Verificar", Status: litodo.InProgress}}}
	source.Plan = &planstate.State{SchemaVersion: planstate.SchemaVersion, Revision: 2, Mode: planstate.Build, LatestPlan: "Terminar importación", Ready: true}
	source.Goal = &ligoal.State{Objective: "Completar migración", Status: ligoal.Interrupted, TokensUsed: 1234}
	source.ForkedFrom = &ForkOrigin{SessionID: "origen-123", ProjectPath: filepath.Join(t.TempDir(), "otro-proyecto"), PointID: "point-1"}
	source.Revision = 17

	path := filepath.Join(t.TempDir(), "chat.jsonl")
	stats, err := ExportJSONL(path, source)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if stats.Messages != 2 || stats.Transcript != 2 || stats.Compactions != 1 {
		t.Fatalf("stats export inesperados: %+v", stats)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "projectPath") || strings.Contains(text, sourceProject) || strings.Contains(text, "forkedFrom") || strings.Contains(text, source.ID) {
		t.Fatalf("el export portable filtró identidad/vinculación local:\n%s", text)
	}
	if !strings.Contains(text, `"format":"lilith-chat-jsonl"`) || !strings.Contains(text, `"type":"message"`) {
		t.Fatalf("formato JSONL inesperado:\n%s", text)
	}

	imported, importedStats, err := ImportJSONL(path, destinationProject)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.ProjectPath != filepath.Clean(destinationProject) {
		t.Fatalf("project path importado=%q, esperado=%q", imported.ProjectPath, filepath.Clean(destinationProject))
	}
	if imported.ID == source.ID || strings.TrimSpace(imported.ID) == "" {
		t.Fatalf("la importación debe crear ID local nuevo: source=%q imported=%q", source.ID, imported.ID)
	}
	if imported.ForkedFrom != nil || imported.Live != nil || imported.Revision != 0 {
		t.Fatalf("estado local no portable sobrevivió: fork=%+v live=%+v revision=%d", imported.ForkedFrom, imported.Live, imported.Revision)
	}
	if imported.Title != source.Title || len(imported.Messages) != 2 || len(imported.Transcript) != 2 || len(imported.Compactions) != 1 {
		t.Fatalf("contenido importado incompleto: %+v", imported)
	}
	if imported.Todo == nil || imported.Todo.Revision != 4 || imported.Plan == nil || imported.Plan.LatestPlan != "Terminar importación" || imported.Goal == nil || imported.Goal.Objective != "Completar migración" {
		t.Fatalf("progreso no restaurado: todo=%+v plan=%+v goal=%+v", imported.Todo, imported.Plan, imported.Goal)
	}
	if importedStats != stats {
		t.Fatalf("stats import=%+v export=%+v", importedStats, stats)
	}
}

func TestPortableImportRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	data := "{\"type\":\"meta\",\"format\":\"lilith-chat-jsonl\",\"version\":999}\n" +
		"{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":\"hola\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportJSONL(path, t.TempDir()); err == nil || !strings.Contains(err.Error(), "versión") {
		t.Fatalf("se esperaba rechazo de versión, err=%v", err)
	}
}

func TestPortableExportOverwritesNamedBackupAtomically(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	first := New(project)
	first.Messages = []openai.Message{{Role: "user", Content: "primero"}}
	if _, err := ExportJSONL(path, first); err != nil {
		t.Fatalf("primer export: %v", err)
	}
	second := New(project)
	second.Messages = []openai.Message{{Role: "user", Content: "segundo"}}
	if _, err := ExportJSONL(path, second); err != nil {
		t.Fatalf("segundo export: %v", err)
	}
	imported, _, err := ImportJSONL(path, project)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Messages) != 1 || imported.Messages[0].Content != "segundo" {
		t.Fatalf("el backup no fue reemplazado: %+v", imported.Messages)
	}
}

func TestPortableImportIgnoresForeignProjectPathField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	foreign := filepath.Join("C:", "Users", "Otro", "Proyecto")
	data := "{\"type\":\"meta\",\"format\":\"lilith-chat-jsonl\",\"version\":1,\"projectPath\":\"" + strings.ReplaceAll(foreign, "\\", "\\\\") + "\"}\n" +
		"{\"type\":\"message\",\"message\":{\"role\":\"user\",\"content\":\"hola\"},\"projectPath\":\"" + strings.ReplaceAll(foreign, "\\", "\\\\") + "\"}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	current := t.TempDir()
	imported, _, err := ImportJSONL(path, current)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.ProjectPath != filepath.Clean(current) {
		t.Fatalf("un projectPath externo no debe imponerse: got=%q want=%q", imported.ProjectPath, filepath.Clean(current))
	}
}

func TestPortableRoundTripRepairsNestedToolArguments(t *testing.T) {
	project := t.TempDir()
	call := openai.ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "run_terminal_command"
	call.Function.Arguments = "{\"command\":\"uno\ndos\"}"
	source := New(project)
	source.Messages = []openai.Message{
		{Role: "assistant", ToolCalls: []openai.ToolCall{call}},
		{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: "ok"},
	}

	path := filepath.Join(t.TempDir(), "chat.jsonl")
	if _, err := ExportJSONL(path, source); err != nil {
		t.Fatalf("export: %v", err)
	}
	imported, _, err := ImportJSONL(path, project)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Messages) != 2 || len(imported.Messages[0].ToolCalls) != 1 {
		t.Fatalf("historial importado inesperado: %+v", imported.Messages)
	}
	args := imported.Messages[0].ToolCalls[0].Function.Arguments
	if !json.Valid([]byte(args)) {
		t.Fatalf("tool arguments importados siguen inválidos: %q", args)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["command"] != "uno\ndos" {
		t.Fatalf("el saneamiento cambió el comando: %#v", decoded)
	}
}
