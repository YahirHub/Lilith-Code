// Package openai implements a minimal OpenAI-compatible chat client with
// streaming (SSE) support. Used by every provider Lilith talks to.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/secrets"
)

// ToolCall is one function call requested by the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"-"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Message is one entry of the chat history sent to the API.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

// Request configures a single chat call.
type Request struct {
	Provider providers.Provider
	Model    string
	Messages []Message
	Stream   bool
	// Tools are the JSON-schema tool definitions offered to the model.
	Tools []any
	// ReasoningEffort is a turn-scoped compatibility override used by Claude
	// skills/subagents. Empty means provider/model default.
	ReasoningEffort string
}

// Client makes chat completions against any OpenAI-compatible endpoint.
type Client struct {
	HTTP *http.Client
	Dir  string // config dir (to load secrets)
}

// NewClient returns a client with a sane default HTTP timeout.
func NewClient(dir string) *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 5 * time.Minute},
		Dir:  dir,
	}
}

type chatChoice struct {
	Delta struct {
		Content          string            `json:"content"`
		ReasoningContent string            `json:"reasoning_content"`
		Reasoning        string            `json:"reasoning"`
		Thinking         string            `json:"thinking"`
		Analysis         string            `json:"analysis"`
		Thought          string            `json:"thought"`
		ReasoningDetails []reasoningDetail `json:"reasoning_details"`
		Role             string            `json:"role"`
		ToolCalls        []deltaToolCall   `json:"tool_calls"`
	} `json:"delta"`
	Message struct {
		Content          string            `json:"content"`
		ReasoningContent string            `json:"reasoning_content"`
		Reasoning        string            `json:"reasoning"`
		Thinking         string            `json:"thinking"`
		Analysis         string            `json:"analysis"`
		Thought          string            `json:"thought"`
		ReasoningDetails []reasoningDetail `json:"reasoning_details"`
		Role             string            `json:"role"`
		ToolCalls        []ToolCall        `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type reasoningDetail struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Summary string `json:"summary"`
	Content string `json:"content"`
	// Data may contain encrypted/carry-forward reasoning. It must never be
	// rendered as readable thought text.
	Data string `json:"data"`
}

func visibleReasoning(content, reasoning, thinking, analysis, thought string, details []reasoningDetail) string {
	// OpenAI-compatible gateways use several scalar aliases. Prefer exactly one
	// to avoid duplicating tokens when a gateway supplies aliases together.
	for _, candidate := range []string{content, reasoning, thinking, analysis, thought} {
		if candidate != "" {
			return candidate
		}
	}
	var b strings.Builder
	for _, detail := range details {
		part := detail.Text
		if part == "" {
			part = detail.Summary
		}
		if part == "" {
			part = detail.Content
		}
		if part == "" {
			continue
		}
		b.WriteString(part)
	}
	return b.String()
}

type deltaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
	// Algunos gateways (p.ej. OpenRouter Console) devuelven el error como
	// objeto plano {"message":"...","code":"..."} en lugar del wrapper
	// {"error":{...}}. Los capturamos aquí para no ahogar el mensaje.
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Chunk represents one incremental piece of an assistant response.
type Chunk struct {
	Delta     string
	ToolCalls []ToolCall
	// Partial marca instantáneas incompletas de tool calls: sus argumentos
	// aún se están acumulando. La TUI las usa para pintar en vivo.
	Partial bool
	// SupersededIndices lista los `output_index` de tool calls que el
	// backend abandonó a medio streaming (p. ej. Codex reintenta un
	// str_replace atascado emitiendo otro function_call en un índice
	// nuevo sin cerrar el anterior con `output_item.done`). La TUI usa
	// esto para colapsar el panel huérfano en vez de dejarlo eternamente
	// en "escribiendo…".
	SupersededIndices []int
	// Thinking es un delta del resumen de razonamiento (reasoning summary)
	// que algunos modelos (p. ej. gpt-5) emiten antes del texto final.
	Thinking     string
	ThinkingDone bool
	Done         bool
	Err          error
}

// resolveAPIKey returns the effective bearer token for a provider.
func (c *Client) resolveAPIKey(p providers.Provider) (string, error) {
	switch p.Auth {
	case providers.AuthNone, providers.AuthBundled:
		return "", nil
	case providers.AuthEnv:
		v := os.Getenv(p.APIKeyEnv)
		if v == "" {
			return "", fmt.Errorf("La variable de entorno %s no está definida.", p.APIKeyEnv)
		}
		return v, nil
	case providers.AuthAPIKey:
		st, err := secrets.Load(c.Dir)
		if err != nil {
			return "", err
		}
		if k, ok := st.APIKeys[p.ID]; ok && k != "" {
			return k, nil
		}
		return "", fmt.Errorf("No hay API key guardada para %s. Usa /login.", p.Name)
	case providers.AuthOAuth:
		st, err := secrets.Load(c.Dir)
		if err != nil {
			return "", err
		}
		tok, ok := st.OAuth[p.ID]
		if !ok || tok.AccessToken == "" {
			return "", fmt.Errorf("Sesión OAuth ausente para %s.", p.Name)
		}
		return tok.AccessToken, nil
	}
	return "", nil
}

// maxAttempts es el número total de intentos ante fallos transitorios
// (HTTP 5xx, 429 o cortes de red) antes de rendirse.
const maxAttempts = 3

// Stream sends a chat request and pushes chunks into the returned channel.
// Cancel via the context. Reintenta con espera creciente cuando el proveedor
// falla de forma transitoria y aún no se emitió nada del turno.
func (c *Client) Stream(ctx context.Context, req Request) <-chan Chunk {
	out := make(chan Chunk, 8)
	go func() {
		defer close(out)
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			counter := &countingSink{out: out}
			err := c.do(ctx, req, counter)
			if err == nil {
				out <- Chunk{Done: true}
				return
			}
			lastErr = err
			// Sólo reintentamos si el turno aún no emitió nada: repetir a
			// medio stream duplicaría contenido o tool calls.
			if counter.n > 0 || ctx.Err() != nil || !isTransient(err) || attempt == maxAttempts {
				break
			}
			select {
			case <-ctx.Done():
				out <- Chunk{Err: ctx.Err()}
				return
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		out <- Chunk{Err: lastErr}
	}()
	return out
}

// countingSink cuenta lo ya entregado a la TUI en el intento actual.
type countingSink struct {
	out chan<- Chunk
	n   int
}

func (s *countingSink) send(ch Chunk) { s.n++; s.out <- ch }

// isTransient distingue fallos recuperables (red, 429, 5xx) de errores
// definitivos como credenciales inválidas o petición mal formada.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"HTTP 500", "HTTP 502", "HTTP 503", "HTTP 504", "HTTP 429", "HTTP 408", "HTTP 520", "HTTP 524"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	if strings.HasPrefix(msg, "HTTP ") {
		return false
	}
	// Errores de transporte: timeouts, EOF, conexión reiniciada.
	low := strings.ToLower(msg)
	for _, frag := range []string{"timeout", "eof", "connection reset", "connection refused", "temporary", "broken pipe", "no such host", "tls"} {
		if strings.Contains(low, frag) {
			return true
		}
	}
	return false
}

func (c *Client) do(ctx context.Context, req Request, out *countingSink) error {
	if IsCodexProvider(req.Provider) {
		return c.streamCodex(ctx, req, out)
	}
	stream := req.Stream && !req.Provider.UseNonStreaming

	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   stream,
	}
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		body["reasoning_effort"] = strings.TrimSpace(req.ReasoningEffort)
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		body["tool_choice"] = "auto"
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(req.Provider.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Provider.Headers {
		httpReq.Header.Set(k, v)
	}
	if key, err := c.resolveAPIKey(req.Provider); err != nil {
		return err
	} else if key != "" {
		header := req.Provider.APIKeyHeader
		if header == "" {
			header = "Authorization"
		}
		prefix := req.Provider.APIKeyPrefix
		if prefix == "" && strings.EqualFold(header, "Authorization") {
			prefix = "Bearer "
		}
		httpReq.Header.Set(header, prefix+key)
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

	if !stream {
		var raw chatResponse
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return err
		}
		if raw.Error != nil {
			return errors.New(raw.Error.Message)
		}
		if raw.Message != "" && len(raw.Choices) == 0 {
			return errors.New(raw.Message)
		}
		if len(raw.Choices) > 0 {
			choice := raw.Choices[0]
			reasoning := visibleReasoning(choice.Message.ReasoningContent, choice.Message.Reasoning, choice.Message.Thinking, choice.Message.Analysis, choice.Message.Thought, choice.Message.ReasoningDetails)
			thinkingActive := false
			structuredSeen := reasoning != ""
			if structuredSeen {
				out.send(Chunk{Thinking: reasoning})
				thinkingActive = true
			}

			var parser reasoningStreamParser
			pieces := append(parser.Feed(choice.Message.Content), parser.Flush()...)
			if err := emitReasoningPieces(ctx, out, pieces, structuredSeen, &thinkingActive); err != nil {
				return err
			}
			if thinkingActive {
				out.send(Chunk{ThinkingDone: true})
				thinkingActive = false
			}
			if len(choice.Message.ToolCalls) > 0 {
				out.send(Chunk{ToolCalls: choice.Message.ToolCalls})
			}
		}
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	pending := map[int]*ToolCall{}
	var pendingOrder []int
	var parser reasoningStreamParser
	thinkingActive := false
	structuredSeen := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[5:])
		if bytes.Equal(payload, []byte("[DONE]")) {
			break
		}
		var raw chatResponse
		if err := json.Unmarshal(payload, &raw); err != nil {
			continue
		}
		if raw.Error != nil {
			return errors.New(raw.Error.Message)
		}
		if raw.Message != "" && len(raw.Choices) == 0 {
			return errors.New(raw.Message)
		}
		if len(raw.Choices) == 0 {
			continue
		}
		choice := raw.Choices[0]
		reasoning := visibleReasoning(choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.Thinking, choice.Delta.Analysis, choice.Delta.Thought, choice.Delta.ReasoningDetails)
		if reasoning != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				out.send(Chunk{Thinking: reasoning})
				thinkingActive = true
				structuredSeen = true
			}
		}
		for _, tc := range choice.Delta.ToolCalls {
			acc, ok := pending[tc.Index]
			if !ok {
				acc = &ToolCall{Type: "function", Index: tc.Index}
				pending[tc.Index] = acc
				pendingOrder = append(pendingOrder, tc.Index)
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Function.Name = tc.Function.Name
			}
			acc.Function.Arguments += tc.Function.Arguments
		}
		if len(choice.Delta.ToolCalls) > 0 {
			if thinkingActive {
				out.send(Chunk{ThinkingDone: true})
				thinkingActive = false
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				out.send(Chunk{ToolCalls: snapshotCalls(pending, pendingOrder), Partial: true})
			}
		}
		if choice.Delta.Content != "" {
			if err := emitReasoningPieces(ctx, out, parser.Feed(choice.Delta.Content), structuredSeen, &thinkingActive); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := emitReasoningPieces(ctx, out, parser.Flush(), structuredSeen, &thinkingActive); err != nil {
		return err
	}
	if thinkingActive {
		out.send(Chunk{ThinkingDone: true})
	}
	if len(pendingOrder) > 0 {
		out.send(Chunk{ToolCalls: snapshotCalls(pending, pendingOrder)})
	}
	return nil
}

func emitReasoningPieces(ctx context.Context, out *countingSink, pieces []reasoningPiece, suppressInlineThinking bool, thinkingActive *bool) error {
	for _, piece := range pieces {
		if piece.Text == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if piece.Thinking {
			if suppressInlineThinking {
				continue
			}
			out.send(Chunk{Thinking: piece.Text})
			*thinkingActive = true
			continue
		}
		if *thinkingActive {
			out.send(Chunk{ThinkingDone: true})
			*thinkingActive = false
		}
		out.send(Chunk{Delta: piece.Text})
	}
	return nil
}

// snapshotCalls copia el estado actual de las tool calls acumuladas.
func snapshotCalls(pending map[int]*ToolCall, order []int) []ToolCall {
	calls := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		calls = append(calls, *pending[idx])
	}
	return calls
}

// ListModels attempts to discover models exposed by an OpenAI-compatible endpoint.
func (c *Client) ListModels(ctx context.Context, p providers.Provider) ([]string, error) {
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if key, err := c.resolveAPIKey(p); err == nil && key != "" {
		header := p.APIKeyHeader
		if header == "" {
			header = "Authorization"
		}
		prefix := p.APIKeyPrefix
		if prefix == "" && strings.EqualFold(header, "Authorization") {
			prefix = "Bearer "
		}
		httpReq.Header.Set(header, prefix+key)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d al listar modelos", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}
