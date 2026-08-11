package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lilith/li/internal/providers"
)

func TestSanitizeToolCallArgumentsRepairsLiteralControlsInsideStrings(t *testing.T) {
	raw := "{\"command\":\"line1\nline2\x00\tfin\",\"path\":\"a\\\\b\"}"
	got := SanitizeToolCallArguments(raw)

	if !json.Valid([]byte(got)) {
		t.Fatalf("argumentos saneados siguen siendo JSON inválido: %q", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["command"] != "line1\nline2\x00\tfin" {
		t.Fatalf("se alteró el valor semántico: %#v", decoded["command"])
	}
}

func TestSanitizeToolCallArgumentsPreservesValidObject(t *testing.T) {
	raw := `{"command":"Get-ChildItem | Format-Table -AutoSize -Wrap","timeout":30}`
	if got := SanitizeToolCallArguments(raw); got != raw {
		t.Fatalf("JSON válido alterado:\nwant=%q\n got=%q", raw, got)
	}
}

func TestSanitizeToolCallArgumentsFallsBackForIrrecoverableHistory(t *testing.T) {
	for _, raw := range []string{`{"command":`, `["not","object"]`, `null`, `42`} {
		if got := SanitizeToolCallArguments(raw); got != "{}" {
			t.Fatalf("raw=%q got=%q, esperado objeto vacío", raw, got)
		}
	}
}

func TestSanitizeMessagesDoesNotMutateOriginal(t *testing.T) {
	call := ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "shell"
	call.Function.Arguments = "{\"command\":\"uno\ndos\"}"
	original := []Message{{Role: "assistant", ToolCalls: []ToolCall{call}}}

	got := SanitizeMessages(original)
	if original[0].ToolCalls[0].Function.Arguments == got[0].ToolCalls[0].Function.Arguments {
		t.Fatal("la prueba necesita que el original sea inválido y la copia reparada")
	}
	if !json.Valid([]byte(got[0].ToolCalls[0].Function.Arguments)) {
		t.Fatalf("copia no reparada: %q", got[0].ToolCalls[0].Function.Arguments)
	}
	if original[0].ToolCalls[0].Function.Arguments != "{\"command\":\"uno\ndos\"}" {
		t.Fatal("SanitizeMessages mutó el historial original")
	}
}

func TestClientSanitizesHistoricalToolArgumentsBeforeRequest(t *testing.T) {
	bad := ToolCall{ID: "call-old", Type: "function"}
	bad.Function.Name = "run_terminal_command"
	bad.Function.Arguments = "{\"command\":\"Write-Output uno\nWrite-Output dos\"}"

	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Messages) != 2 || len(body.Messages[0].ToolCalls) != 1 {
			t.Fatalf("historial inesperado: %#v", body.Messages)
		}
		received = body.Messages[0].ToolCalls[0].Function.Arguments
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	out := make(chan Chunk, 4)
	err := client.do(context.Background(), Request{
		Provider: providers.Provider{ID: "test", BaseURL: server.URL, Auth: providers.AuthNone, UseNonStreaming: true},
		Model:    "test",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{bad}},
			{Role: "tool", ToolCallID: bad.ID, Name: bad.Function.Name, Content: "ok"},
		},
		Stream: true,
	}, &countingSink{out: out})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(received)) {
		t.Fatalf("el proveedor recibió function.arguments inválido: %q", received)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(received), &args); err != nil {
		t.Fatal(err)
	}
	if args["command"] != "Write-Output uno\nWrite-Output dos" {
		t.Fatalf("argumentos cambiaron de significado: %#v", args)
	}
}

func TestStreamSanitizesCompletedToolCallBeforeExposingIt(t *testing.T) {
	rawArguments := "{\"command\":\"uno\ndos\"}"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		payload, err := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id":    "call-new",
						"type":  "function",
						"function": map[string]any{
							"name":      "run_terminal_command",
							"arguments": rawArguments,
						},
					}},
				},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	chunks := collectChunks(client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", BaseURL: server.URL, Auth: providers.AuthNone},
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "x"}},
		Stream:   true,
	}))

	var final *ToolCall
	for _, chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if !chunk.Partial && len(chunk.ToolCalls) == 1 {
			call := chunk.ToolCalls[0]
			final = &call
		}
	}
	if final == nil {
		t.Fatalf("no llegó tool call final: %#v", chunks)
	}
	if !json.Valid([]byte(final.Function.Arguments)) {
		t.Fatalf("tool call final quedó contaminada: %q", final.Function.Arguments)
	}
}

func TestCodexPayloadSanitizesHistoricalToolArguments(t *testing.T) {
	bad := ToolCall{ID: "call-codex", Type: "function"}
	bad.Function.Name = "run_terminal_command"
	bad.Function.Arguments = "{\"command\":\"uno\ndos\"}"

	payload, err := buildCodexPayload(Request{Messages: []Message{{Role: "assistant", ToolCalls: []ToolCall{bad}}}})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Input []struct {
			Type      string `json:"type"`
			Arguments string `json:"arguments"`
		} `json:"input"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	var arguments string
	for _, item := range body.Input {
		if item.Type == "function_call" {
			arguments = item.Arguments
			break
		}
	}
	if arguments == "" {
		t.Fatalf("payload Codex sin function_call: %s", payload)
	}
	if !json.Valid([]byte(arguments)) {
		t.Fatalf("Codex recibió argumentos inválidos: %q", arguments)
	}
}
