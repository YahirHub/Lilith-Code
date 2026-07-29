package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers/openai"
)

func makeToolCall(name, args string) openai.ToolCall {
	var call openai.ToolCall
	call.ID = "call-test"
	call.Index = 0
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

func TestSummarizeToolResultsUsesCreateOutput(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "create_file",
		Content: "Escrito ejemplo.html (1121 bytes).",
	}})
	if got != "Escrito ejemplo.html (1121 bytes)." {
		t.Fatalf("resumen inesperado: %q", got)
	}
}

func TestSummarizeToolResultsKeepsToolErrors(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "read_files",
		Content: "error: archivo no encontrado",
	}})
	if !strings.Contains(got, "archivo no encontrado") {
		t.Fatalf("debe conservar errores de herramientas: %q", got)
	}
}

func TestSummarizeToolResultsIgnoresReadOnlyNoise(t *testing.T) {
	got := summarizeToolResults([]openai.Message{{
		Role:    "tool",
		Name:    "read_files",
		Content: "== README.md ==\ncontenido largo",
	}})
	if got != "" {
		t.Fatalf("no debe convertir lecturas normales en respuesta final: %q", got)
	}
}

func TestPartialToolCallShowsWorkingImmediately(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	primeTestRequest(t, &m)
	m.thinking = true

	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{
		toolCalls: []openai.ToolCall{makeToolCall("create_file", `{"path":"demo.html","content":"<h1>hola</h1>"}`)},
		partial:   true,
	}))

	if m.thinking {
		t.Fatal("una tool call parcial debe salir del estado pensando")
	}
	if !m.working {
		t.Fatal("una tool call parcial debe activar trabajando desde el primer snapshot")
	}

	view := stripANSI(m.View())
	if !strings.Contains(view, "Trabajando") {
		t.Fatalf("el indicador fijo debe seguir visible durante argumentos parciales:\n%s", view)
	}

	// El timer del shimmer no debe morir durante el intervalo parcial. Éste
	// era el fallo real: thinking=false + working=false hacía que el siguiente
	// tick devolviera nil y la animación se apagara hasta finalizar la call.
	_, cmd := m.Update(thinkingTickMsg{frame: 1})
	if cmd == nil {
		t.Fatal("el shimmer debe seguir programando frames mientras working=true")
	}
}

func TestReasoningKeepsThinkingIndicatorActive(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme())}
	m := NewChat(ctx)
	m.Resize(100, 30)
	primeTestRequest(t, &m)
	m.thinking = true

	_, _ = m.Update(activeStreamMsg(&m, chatStreamMsg{thinking: "Analizando el cambio..."}))

	if !m.thinking || m.working {
		t.Fatalf("reasoning debe mantener Pensando activo: thinking=%v working=%v", m.thinking, m.working)
	}
	if !strings.Contains(stripANSI(m.View()), "Pensando") {
		t.Fatal("el indicador Pensando debe permanecer visible mientras llega reasoning")
	}
}

func TestCompactRejectedCreateCallDropsUnappliedPayload(t *testing.T) {
	call := makeToolCall("create_file", `{"path":"styles.css","content":"`+strings.Repeat("x", 8000)+`"}`)
	m := ChatModel{history: []openai.Message{{Role: "assistant", ToolCalls: []openai.ToolCall{call}}}}
	m.compactRejectedCreateCall(call.ID)
	got := m.history[0].ToolCalls[0].Function.Arguments
	if strings.Contains(got, strings.Repeat("x", 100)) {
		t.Fatalf("rejected payload should not remain in API history: %d bytes", len(got))
	}
	if !strings.Contains(got, `"path":"styles.css"`) || !strings.Contains(got, `"content":""`) {
		t.Fatalf("unexpected compact arguments: %s", got)
	}
}

func TestPreflightStreamingCreateCallRejectsExistingPathBeforeBodyCompletes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "styles.css")
	if err := os.WriteFile(path, []byte("body { color: red; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := makeToolCall("create_file", `{"path":"styles.css","content":"`+strings.Repeat("x", 200))
	got, result, ok := preflightStreamingCreateCall(root, call)
	if !ok {
		t.Fatal("existing target should be intercepted from partial create_file arguments")
	}
	if !strings.HasPrefix(result, "FILE_EXISTS:") {
		t.Fatalf("unexpected preflight result: %q", result)
	}
	if strings.Contains(got.Function.Arguments, strings.Repeat("x", 20)) {
		t.Fatalf("synthetic call must discard the streamed body: %s", got.Function.Arguments)
	}
	if !strings.Contains(got.Function.Arguments, `"path":"styles.css"`) || !strings.Contains(got.Function.Arguments, `"content":""`) {
		t.Fatalf("unexpected compact call arguments: %s", got.Function.Arguments)
	}
}

func TestPreflightStreamingCreateCallAllowsMissingPath(t *testing.T) {
	root := t.TempDir()
	call := makeToolCall("create_file", `{"path":"new.css","content":"partial`)
	_, _, ok := preflightStreamingCreateCall(root, call)
	if ok {
		t.Fatal("missing target must continue streaming normally")
	}
}

func TestSwitchCreateToolToEditorsAfterFileExists(t *testing.T) {
	m := ChatModel{activeTools: []string{"tool_search", "create_file"}}
	m.switchCreateToolToEditors()
	joined := strings.Join(m.activeTools, ",")
	if strings.Contains(joined, "create_file") || strings.Contains(joined, "write_file") {
		t.Fatalf("create tool must be removed after FILE_EXISTS: %v", m.activeTools)
	}
	for _, want := range []string{"read_files", "str_replace", "apply_diff"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s after FILE_EXISTS recovery: %v", want, m.activeTools)
		}
	}
}

func TestSystemPromptKeepsStableToolGuidanceAcrossLazyToolSets(t *testing.T) {
	one := systemPrompt([]string{"str_replace"}, "", "", "", "")
	two := systemPrompt([]string{"str_replace", "run_terminal_command", "create_file"}, "", "", "", "")
	if one != two {
		t.Fatalf("lazy tool materialization must not rewrite the reusable system prefix:\n--- one ---\n%s\n--- two ---\n%s", one, two)
	}
	for _, want := range []string{
		"validate against the current on-disk file",
		"`write` and `write_file` are unsupported legacy names",
		"FILE_EXISTS, USE_CREATE_FILE and WRITE_BLOCKED",
	} {
		if !strings.Contains(one, want) {
			t.Fatalf("stable tool guidance missing %q:\n%s", want, one)
		}
	}
	if strings.Contains(one, "str_replace: Make precise replacements") {
		t.Fatalf("active-only promptSnippet should not enter the cacheable system prefix:\n%s", one)
	}
}

func TestPreflightStreamingLegacyWriteExistingStopsBeforeBodyCompletes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := makeToolCall("write_file", `{"path":"styles.css","content":"`+strings.Repeat("x", 200))
	got, result, ok := preflightStreamingCreateCall(root, call)
	if !ok || !strings.HasPrefix(result, "FILE_EXISTS:") {
		t.Fatalf("legacy write to existing file must be intercepted: ok=%v result=%q", ok, result)
	}
	if strings.Contains(got.Function.Arguments, strings.Repeat("x", 20)) {
		t.Fatalf("rejected legacy body leaked into compact call: %s", got.Function.Arguments)
	}
}

func TestPreflightStreamingLegacyWriteMissingRedirectsToCreateFile(t *testing.T) {
	root := t.TempDir()
	call := makeToolCall("write", `{"path":"new.css","content":"partial`)
	got, result, ok := preflightStreamingCreateCall(root, call)
	if !ok || !strings.HasPrefix(result, "USE_CREATE_FILE:") {
		t.Fatalf("legacy write to a new path must redirect to create_file: ok=%v result=%q", ok, result)
	}
	if !strings.Contains(got.Function.Arguments, `"content":""`) {
		t.Fatalf("legacy call should be compacted before continuation: %s", got.Function.Arguments)
	}
}

func TestFilePanelTreatsLegacyWriteInterceptionAsSkipped(t *testing.T) {
	p := &FilePanel{Tool: "write_file", Path: "styles.css", Content: strings.Repeat("x\n", 50)}
	p.Finish("FILE_EXISTS: styles.css already exists. Use str_replace or apply_diff.")
	if !p.Done || !p.Skipped || p.Failed {
		t.Fatalf("legacy interception should be a recoverable skip: %+v", p)
	}
}

func TestPreflightWaitsForCompletePathString(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node-ocr", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The partial prefix happens to be an existing directory. Preflight must not
	// mistake it for the final target until the closing quote for path arrives.
	call := makeToolCall("create_file", `{"path":"node-ocr/src`)
	_, _, ok := preflightStreamingCreateCall(root, call)
	if ok {
		t.Fatal("an incomplete path string must never trigger filesystem preflight")
	}
}

func TestPartialJSONStringStillSupportsLivePreview(t *testing.T) {
	value, ok := partialJSONString(`{"content":"abc`, "content")
	if !ok || value != "abc" {
		t.Fatalf("live preview should keep partial string support: ok=%v value=%q", ok, value)
	}
	if _, ok := completeJSONString(`{"content":"abc`, "content"); ok {
		t.Fatal("strict extraction must reject an unterminated JSON string")
	}
}

func TestLegacyWriteIsStoppedAsSoonAsToolNameIsKnown(t *testing.T) {
	root := t.TempDir()
	call := makeToolCall("write_file", `{"content":"`+strings.Repeat("x", 200))
	got, result, ok := preflightStreamingCreateCall(root, call)
	if !ok || !strings.HasPrefix(result, "WRITE_BLOCKED:") {
		t.Fatalf("legacy write should be rejected before path/body completes: ok=%v result=%q", ok, result)
	}
	if strings.Contains(got.Function.Arguments, strings.Repeat("x", 20)) {
		t.Fatalf("legacy body should be discarded immediately: %s", got.Function.Arguments)
	}
}
