// Package openai — transporte específico para el backend Codex de ChatGPT.
//
// La suscripción ChatGPT Plus/Pro no expone `/v1/chat/completions` sino la
// Responses API en https://chatgpt.com/backend-api/codex/responses. Este
// archivo traduce el formato interno (chat/completions con tool_calls) al
// esquema Responses y viceversa a los `Chunk` que consume la TUI.
package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/secrets"
)

// Cabeceras y valores exigidos por el backend Codex.
const (
	codexOriginator      = "codex_cli_rs"
	codexOpenAIBeta      = "responses=experimental"
	codexResponsesSuffix = "/responses"
)

var codexSessionOnce sync.Once
var codexSessionID string

func codexSession() string {
	codexSessionOnce.Do(func() {
		var buf [16]byte
		if _, err := rand.Read(buf[:]); err == nil {
			// UUIDv4 sencillo.
			buf[6] = (buf[6] & 0x0f) | 0x40
			buf[8] = (buf[8] & 0x3f) | 0x80
			codexSessionID = fmt.Sprintf("%s-%s-%s-%s-%s",
				hex.EncodeToString(buf[0:4]),
				hex.EncodeToString(buf[4:6]),
				hex.EncodeToString(buf[6:8]),
				hex.EncodeToString(buf[8:10]),
				hex.EncodeToString(buf[10:16]),
			)
		} else {
			codexSessionID = "lilith-session"
		}
	})
	return codexSessionID
}

// IsCodexProvider indica si un proveedor debe usar la Responses API de Codex.
func IsCodexProvider(p providers.Provider) bool {
	return p.ID == "openai-codex"
}

// streamCodex ejecuta el turno contra la Responses API y va emitiendo Chunks.
// Reutiliza el HTTP client de Client y la lógica de countingSink.
func (c *Client) streamCodex(ctx context.Context, req Request, out *countingSink) error {
	st, err := secrets.Load(c.Dir)
	if err != nil {
		return err
	}
	tok, ok := st.OAuth[req.Provider.ID]
	if !ok || tok.AccessToken == "" {
		return fmt.Errorf("Sesión OAuth ausente para %s.", req.Provider.Name)
	}

	body, err := buildCodexPayload(req)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(req.Provider.BaseURL, "/") + codexResponsesSuffix
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	httpReq.Header.Set("OpenAI-Beta", codexOpenAIBeta)
	httpReq.Header.Set("originator", codexOriginator)
	httpReq.Header.Set("session_id", codexSession())
	if tok.AccountID != "" {
		httpReq.Header.Set("chatgpt-account-id", tok.AccountID)
	}
	for k, v := range req.Provider.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return parseCodexSSE(ctx, resp.Body, c.streamIdleTimeout(), out)
}

// buildCodexPayload transforma Messages/Tools estilo chat/completions al
// esquema Responses.
func buildCodexPayload(req Request) ([]byte, error) {
	req.Messages = SanitizeMessages(req.Messages)
	var instructions strings.Builder
	input := make([]map[string]any, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if instructions.Len() > 0 {
				instructions.WriteString("\n\n")
			}
			instructions.WriteString(m.Content)
		case "user":
			input = append(input, map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": m.Content},
				},
			})
		case "assistant":
			if m.Content != "" {
				input = append(input, map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": m.Content},
					},
				})
			}
			for _, tc := range m.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
				if name == "" {
					// Codex Responses rechaza input[i].name vacío con HTTP 400
					// (empty_string). Ignoramos tool_calls incompletos que
					// pudieran haber llegado por SSE parcial.
					continue
				}
				callID := tc.ID
				if callID == "" {
					callID = name
				}
				args := tc.Function.Arguments
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      name,
					"arguments": args,
				})
			}
		case "tool":
			callID := m.ToolCallID
			if callID == "" {
				callID = m.Name
			}
			if strings.TrimSpace(callID) == "" {
				// Sin call_id el backend no puede emparejar la salida; lo
				// omitimos para no romper la conversación entera.
				continue
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  m.Content,
			})

		}
	}

	// Defensa en profundidad: Codex Responses rechaza el request con
	// HTTP 400 "No tool output found for function call ..." si un
	// function_call en `input` no tiene su function_call_output pareja.
	// Esto ocurre cuando un turno se cortó a la mitad (cancelación del
	// usuario, cierre/reemplazo de sesión o tool call abandonada por un
	// reintento del backend). Inyectamos un output stub para cada function_call huérfano
	// para que la conversación pueda continuar.
	haveOutput := map[string]bool{}
	for _, it := range input {
		if it["type"] == "function_call_output" {
			if cid, ok := it["call_id"].(string); ok {
				haveOutput[cid] = true
			}
		}
	}
	patched := make([]map[string]any, 0, len(input))
	for _, it := range input {
		patched = append(patched, it)
		if it["type"] != "function_call" {
			continue
		}
		cid, _ := it["call_id"].(string)
		if cid == "" || haveOutput[cid] {
			continue
		}
		patched = append(patched, map[string]any{
			"type":    "function_call_output",
			"call_id": cid,
			"output":  "error: la ejecución de esta herramienta no se completó (turno interrumpido).",
		})
		haveOutput[cid] = true
	}
	input = patched

	payload := map[string]any{
		"model":               req.Model,
		"instructions":        instructions.String(),
		"input":               input,
		"tool_choice":         "auto",
		"parallel_tool_calls": false,
		"store":               false,
		"stream":              true,
		"include":             []string{},
	}

	// Modelos de razonamiento (gpt-5.*): pedimos resumen breve. El esfuerzo
	// puede venir de una skill/subagente Claude y es estrictamente turn-scoped.
	reasoning := map[string]any{"summary": "auto"}
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		reasoning["effort"] = strings.TrimSpace(req.ReasoningEffort)
	}
	payload["reasoning"] = reasoning

	if len(req.Tools) > 0 {
		payload["tools"] = convertToolsToResponses(req.Tools)
	}
	return json.Marshal(payload)
}

// convertToolsToResponses aplana `{type:function, function:{...}}` a la forma
// plana que espera la Responses API: `{type:function, name, description,
// parameters, strict}`.
func convertToolsToResponses(tools []any) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, raw := range tools {
		m, ok := raw.(map[string]any)
		if !ok {
			// Intentamos un roundtrip vía JSON por si es un struct tipado.
			buf, err := json.Marshal(raw)
			if err != nil {
				continue
			}
			m = map[string]any{}
			if err := json.Unmarshal(buf, &m); err != nil {
				continue
			}
		}
		fn, _ := m["function"].(map[string]any)
		if fn == nil {
			// Ya está aplanada.
			out = append(out, m)
			continue
		}
		entry := map[string]any{"type": "function"}
		if v, ok := fn["name"]; ok {
			entry["name"] = v
		}
		if v, ok := fn["description"]; ok {
			entry["description"] = v
		}
		if v, ok := fn["parameters"]; ok {
			entry["parameters"] = v
		}
		if v, ok := fn["strict"]; ok {
			entry["strict"] = v
		} else {
			entry["strict"] = false
		}
		out = append(out, entry)
	}
	return out
}

// parseCodexSSE lee eventos SSE de la Responses API y los traduce a Chunks.
//
// La Responses API identifica cada bloque de salida por `output_index` (0, 1,
// 2, …). Ese índice es la ÚNICA clave estable entre `response.output_item.*`
// y `response.function_call_arguments.*`: `call_id` sólo aparece en
// `output_item.*`, mientras que los deltas de argumentos usan `item_id`
// (`fc_…`). Antes indexábamos por `call_id`, así que los deltas creaban un
// pending nuevo sin nombre, `snapshotCodex` lo descartaba y las ediciones
// (`str_replace`, que emite argumentos grandes vía deltas) sólo aparecían al
// final del turno.
func parseCodexSSE(ctx context.Context, body io.ReadCloser, idle time.Duration, out *countingSink) error {
	pending := map[int]*ToolCall{}
	// byItemID / byCallID son alias para tolerar eventos raros que sólo traen
	// uno de los identificadores en lugar de `output_index`.
	byItemID := map[string]int{}
	byCallID := map[string]int{}
	var order []int
	// doneIdx registra los output_index de tool calls que ya recibieron su
	// `response.output_item.done`. Con `parallel_tool_calls=false`, cualquier
	// nuevo function_call que aparezca mientras otro pending sigue sin done
	// es un reintento server-side: hay que marcar el anterior como abandonado
	// para que la TUI colapse su panel "escribiendo…".
	doneIdx := map[int]bool{}

	getPendingIdx := func(idx int) *ToolCall {
		if tc, ok := pending[idx]; ok {
			return tc
		}
		tc := &ToolCall{Type: "function", Index: idx}
		pending[idx] = tc
		order = append(order, idx)
		return tc
	}
	// resolveIdx localiza el output_index a partir de los ids conocidos y, si
	// aún no existe, crea un slot nuevo. Nunca devuelve -1: garantiza que
	// cada tool call termine con un pending consistente.
	resolveIdx := func(outputIdx *int, itemID, callID string) int {
		if outputIdx != nil {
			return *outputIdx
		}
		if itemID != "" {
			if idx, ok := byItemID[itemID]; ok {
				return idx
			}
		}
		if callID != "" {
			if idx, ok := byCallID[callID]; ok {
				return idx
			}
		}
		// Fallback: nuevo slot al final.
		return len(order)
	}

	err := scanResponseLines(ctx, body, idle, func(line []byte) error {
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			return nil
		}
		payload := bytes.TrimSpace(line[5:])
		if bytes.Equal(payload, []byte("[DONE]")) {
			return errSSEDone
		}
		var ev struct {
			Type        string          `json:"type"`
			Delta       string          `json:"delta"`
			ItemID      string          `json:"item_id"`
			CallID      string          `json:"call_id"`
			OutputIndex *int            `json:"output_index"`
			Item        json.RawMessage `json:"item"`
			Response    json.RawMessage `json:"response"`
			Arguments   string          `json:"arguments"`
		}
		if err := json.Unmarshal(payload, &ev); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				out.send(Chunk{Delta: ev.Delta})
			}
		case "response.reasoning_summary_text.delta",
			"response.reasoning_text.delta",
			"response.reasoning.delta":
			if ev.Delta != "" {
				out.send(Chunk{Thinking: ev.Delta})
			}
		case "response.reasoning_summary_text.done",
			"response.reasoning_text.done",
			"response.reasoning.done":
			out.send(Chunk{ThinkingDone: true})
		case "response.output_item.added":
			var item struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal(ev.Item, &item); err != nil {
				return nil
			}
			if item.Type != "function_call" {
				return nil
			}
			idx := resolveIdx(ev.OutputIndex, item.ID, item.CallID)
			// Si aparece un function_call NUEVO mientras hay pendings previos
			// que nunca vieron su `output_item.done`, son reintentos huérfanos
			// del backend (Codex reemite el tool call en otro output_index
			// cuando el anterior se atascó). Los sacamos de `order` y avisamos
			// a la TUI con SupersededIndices para que colapse esos paneles.
			var superseded []int
			if _, exists := pending[idx]; !exists {
				for _, prev := range order {
					if prev == idx || doneIdx[prev] {
						continue
					}
					prevTC := pending[prev]
					if prevTC == nil || strings.TrimSpace(prevTC.Function.Name) == "" {
						continue
					}
					superseded = append(superseded, prev)
					doneIdx[prev] = true
				}
				if len(superseded) > 0 {
					filtered := order[:0]
					supSet := map[int]bool{}
					for _, x := range superseded {
						supSet[x] = true
					}
					for _, o := range order {
						if !supSet[o] {
							filtered = append(filtered, o)
						}
					}
					order = filtered
				}
			}
			tc := getPendingIdx(idx)
			if item.ID != "" {
				byItemID[item.ID] = idx
			}
			if item.CallID != "" {
				byCallID[item.CallID] = idx
				tc.ID = item.CallID
			} else if tc.ID == "" && item.ID != "" {
				tc.ID = item.ID
			}
			if item.Name != "" {
				tc.Function.Name = item.Name
			}
			if item.Arguments != "" {
				tc.Function.Arguments += item.Arguments
			}
			// Emitimos ya el snapshot para que la TUI cree el panel de
			// archivo antes de que empiecen a llegar los deltas: así el
			// usuario ve la caja "escribiendo…" desde el primer instante.
			if strings.TrimSpace(tc.Function.Name) != "" {
				out.send(Chunk{ToolCalls: snapshotCodex(pending, order), Partial: true, SupersededIndices: superseded})
			} else if len(superseded) > 0 {
				out.send(Chunk{SupersededIndices: superseded})
			}
		case "response.function_call_arguments.delta":
			if ev.Delta == "" {
				return nil
			}
			idx := resolveIdx(ev.OutputIndex, ev.ItemID, ev.CallID)
			tc := getPendingIdx(idx)
			if ev.ItemID != "" {
				byItemID[ev.ItemID] = idx
			}
			if ev.CallID != "" {
				byCallID[ev.CallID] = idx
				if tc.ID == "" {
					tc.ID = ev.CallID
				}
			}
			tc.Function.Arguments += ev.Delta
			if strings.TrimSpace(tc.Function.Name) != "" {
				out.send(Chunk{ToolCalls: snapshotCodex(pending, order), Partial: true})
			}
		case "response.function_call_arguments.done":
			// El backend cierra el bloque de argumentos con la cadena
			// completa; nos aseguramos de tener el valor final antes del
			// `output_item.done`.
			if ev.Arguments == "" {
				return nil
			}
			idx := resolveIdx(ev.OutputIndex, ev.ItemID, ev.CallID)
			tc := getPendingIdx(idx)
			if len(ev.Arguments) >= len(tc.Function.Arguments) {
				tc.Function.Arguments = ev.Arguments
			}
			if strings.TrimSpace(tc.Function.Name) != "" {
				out.send(Chunk{ToolCalls: snapshotCodex(pending, order), Partial: true})
			}
		case "response.output_item.done":
			var item struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal(ev.Item, &item); err != nil {
				return nil
			}
			if item.Type != "function_call" {
				return nil
			}
			idx := resolveIdx(ev.OutputIndex, item.ID, item.CallID)
			doneIdx[idx] = true
			tc := getPendingIdx(idx)
			if item.CallID != "" {
				tc.ID = item.CallID
				byCallID[item.CallID] = idx
			} else if tc.ID == "" && item.ID != "" {
				tc.ID = item.ID
			}
			if item.Name != "" {
				tc.Function.Name = item.Name
			}
			if item.Arguments != "" && len(item.Arguments) >= len(tc.Function.Arguments) {
				// El backend a veces reemite argumentos completos: sólo
				// sobreescribimos si acumulamos menos que el final.
				tc.Function.Arguments = item.Arguments
			}
		case "response.failed":
			return decodeCodexError(ev.Response)
		case "response.incomplete":
			return errors.New("respuesta Codex incompleta")
		case "response.completed":
			// Nada más que emitir; el bucle terminará y el llamador enviará Done.
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSSEDone) {
		return err
	}

	if len(order) > 0 {
		out.send(Chunk{ToolCalls: SanitizeToolCalls(snapshotCodex(pending, order))})
	}
	return nil
}

func snapshotCodex(pending map[int]*ToolCall, order []int) []ToolCall {
	calls := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		tc := pending[idx]
		if tc == nil || strings.TrimSpace(tc.Function.Name) == "" {
			continue
		}
		calls = append(calls, *tc)
	}
	return calls
}

func decodeCodexError(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("response.failed sin detalles")
	}
	var wrap struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Error.Message != "" {
		return fmt.Errorf("Codex: %s", wrap.Error.Message)
	}
	return errors.New("response.failed")
}
