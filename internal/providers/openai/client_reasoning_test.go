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

	if got := visibleReasoning("cot", "duplicado", "", "", "", nil); got != "cot" {
		t.Fatalf("reasoning_content debe tener prioridad: %q", got)
	}
	if got := visibleReasoning("", "razonamiento", "", "", "", nil); got != "razonamiento" {
		t.Fatalf("alias reasoning no reconocido: %q", got)
	}
	details := []reasoningDetail{{Type: "reasoning.text", Text: "uno "}, {Type: "reasoning.summary", Summary: "dos"}}
	if got := visibleReasoning("", "", "", "", "", details); got != "uno dos" {
		t.Fatalf("reasoning_details no reconocido: %q", got)
	}
}

func TestStreamOpenAICompatibleSeparaThinkInlineDeMiniMax(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Deliberately split both tags across transport chunks.
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"<thi"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"nk>Analizo la petición"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"</th"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ink>¡Hola!"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	chunks := client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "minimax", BaseURL: server.URL, Auth: providers.AuthNone},
		Model:    "MiniMax-M2.1",
		Messages: []Message{{Role: "user", Content: "Hola"}},
		Stream:   true,
	})

	var thinking, final strings.Builder
	var thinkingDone int
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("stream devolvió error: %v", chunk.Err)
		}
		thinking.WriteString(chunk.Thinking)
		final.WriteString(chunk.Delta)
		if chunk.ThinkingDone {
			thinkingDone++
		}
	}
	if got := thinking.String(); got != "Analizo la petición" {
		t.Fatalf("pensamiento inline inesperado: %q", got)
	}
	if got := final.String(); got != "¡Hola!" {
		t.Fatalf("respuesta final inesperada: %q", got)
	}
	if thinkingDone != 1 {
		t.Fatalf("ThinkingDone=%d, se esperaba 1", thinkingDone)
	}
}

func TestReasoningParserReconoceVariantesInline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		thinking string
		final    string
	}{
		{"thinking_xml", "<thinking>uno</thinking>dos", "uno", "dos"},
		{"unicode_antes_del_tag", "前置<think>uno</think>dos", "uno", "前置dos"},
		{"analysis_xml", "<analysis>uno</analysis>dos", "uno", "dos"},
		{"reasoning_xml", "<reasoning>uno</reasoning>dos", "uno", "dos"},
		{"thought_xml", "<thought>uno</thought>dos", "uno", "dos"},
		{"mistral_think", "[THINK]uno[/THINK]dos", "uno", "dos"},
		{"reasoning_brackets", "[REASONING]uno[/REASONING]dos", "uno", "dos"},
		{"analysis_brackets", "[analysis]uno[/analysis]dos", "uno", "dos"},
		{"harmony_channel", "<|channel|>analysis<|message|>uno<|channel|>final<|message|>dos", "uno", "dos"},
		{"harmony_full", "<|start|>assistant<|channel|>analysis<|message|>uno<|end|>dos", "uno", "dos"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var parser reasoningStreamParser
			pieces := append(parser.Feed(tc.input), parser.Flush()...)
			var thinking, final strings.Builder
			for _, piece := range pieces {
				if piece.Thinking {
					thinking.WriteString(piece.Text)
				} else {
					final.WriteString(piece.Text)
				}
			}
			if thinking.String() != tc.thinking || final.String() != tc.final {
				t.Fatalf("thinking=%q final=%q", thinking.String(), final.String())
			}
		})
	}
}

func TestReasoningParserConservaMarcadorIncompletoComoPensamiento(t *testing.T) {
	t.Parallel()
	var parser reasoningStreamParser
	pieces := append(parser.Feed("<think>sin cierre"), parser.Flush()...)
	if len(pieces) != 1 || !pieces[0].Thinking || pieces[0].Text != "sin cierre" {
		t.Fatalf("piezas inesperadas: %#v", pieces)
	}
}

func TestVisibleReasoningAceptaMasAliasYContentDeDetalles(t *testing.T) {
	t.Parallel()
	if got := visibleReasoning("", "", "pensando", "analisis", "idea", nil); got != "pensando" {
		t.Fatalf("alias thinking no reconocido: %q", got)
	}
	if got := visibleReasoning("", "", "", "analisis", "idea", nil); got != "analisis" {
		t.Fatalf("alias analysis no reconocido: %q", got)
	}
	if got := visibleReasoning("", "", "", "", "idea", nil); got != "idea" {
		t.Fatalf("alias thought no reconocido: %q", got)
	}
	details := []reasoningDetail{{Type: "reasoning.text", Content: "contenido"}, {Type: "reasoning.encrypted", Data: "no mostrar"}}
	if got := visibleReasoning("", "", "", "", "", details); got != "contenido" {
		t.Fatalf("content de reasoning_details no reconocido o data filtrado mal: %q", got)
	}
}

func TestStructuredReasoningEvitaDuplicarThinkInline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning":"interno"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"<think>interno</think>final"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	chunks := client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "gateway", BaseURL: server.URL, Auth: providers.AuthNone},
		Model:    "reasoner",
		Messages: []Message{{Role: "user", Content: "x"}},
		Stream:   true,
	})
	var thinking, final strings.Builder
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		thinking.WriteString(chunk.Thinking)
		final.WriteString(chunk.Delta)
	}
	if thinking.String() != "interno" || final.String() != "final" {
		t.Fatalf("thinking=%q final=%q", thinking.String(), final.String())
	}
}

func TestNonStreamingSeparatesInlineReasoning(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","content":"<analysis>reviso</analysis>resultado"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	chunks := client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "non-stream", BaseURL: server.URL, Auth: providers.AuthNone, UseNonStreaming: true},
		Model:    "reasoner",
		Messages: []Message{{Role: "user", Content: "x"}},
		Stream:   true,
	})
	var thinking, final strings.Builder
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		thinking.WriteString(chunk.Thinking)
		final.WriteString(chunk.Delta)
	}
	if thinking.String() != "reviso" || final.String() != "resultado" {
		t.Fatalf("thinking=%q final=%q", thinking.String(), final.String())
	}
}
