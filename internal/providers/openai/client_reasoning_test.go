package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers"
)

func TestStreamOpenAICompatibleExponeReasoningContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("ruta inesperada: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"Analizando "},"finish_reason":null}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"el cambio"},"finish_reason":null}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Listo"},"finish_reason":null}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	ch := client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", BaseURL: server.URL, Auth: providers.AuthNone},
		Model:    "reasoner",
		Messages: []Message{{Role: "user", Content: "prueba"}},
		Stream:   true,
	})

	var reasoning, content strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream devolvió error: %v", chunk.Err)
		}
		reasoning.WriteString(chunk.Thinking)
		content.WriteString(chunk.Delta)
	}

	if got := reasoning.String(); got != "Analizando el cambio" {
		t.Fatalf("reasoning inesperado: %q", got)
	}
	if got := content.String(); got != "Listo" {
		t.Fatalf("contenido inesperado: %q", got)
	}
}

func TestVisibleReasoningAceptaAliasYDetalles(t *testing.T) {
	t.Parallel()

	if got := visibleReasoning("cot", "duplicado", nil); got != "cot" {
		t.Fatalf("reasoning_content debe tener prioridad: %q", got)
	}
	if got := visibleReasoning("", "razonamiento", nil); got != "razonamiento" {
		t.Fatalf("alias reasoning no reconocido: %q", got)
	}
	details := []reasoningDetail{{Type: "reasoning.text", Text: "uno "}, {Type: "reasoning.summary", Summary: "dos"}}
	if got := visibleReasoning("", "", details); got != "uno dos" {
		t.Fatalf("reasoning_details no reconocido: %q", got)
	}
}
