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
	"net"
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

	// StreamIdleTimeout limits only periods without any bytes from an SSE
	// response. It is not a total request timeout, so long active generations
	// remain valid. Zero uses the production default; a negative value disables
	// the watchdog (useful for specialized providers/tests).
	StreamIdleTimeout time.Duration

	// ConnectivityProbe is optional and primarily useful to embedders/tests. The
	// production fallback probes the selected provider first and then public
	// connectivity endpoints to distinguish an offline machine from a provider
	// outage. NetworkRetryMinDelay/MaxDelay bound the recovery polling cadence.
	ConnectivityProbe    func(context.Context, providers.Provider) ConnectivityState
	NetworkRetryMinDelay time.Duration
	NetworkRetryMaxDelay time.Duration
}

const defaultStreamIdleTimeout = 4 * time.Minute

// NewClient returns a streaming-safe client. A global http.Client timeout is
// intentionally avoided because it kills healthy long responses. Instead the
// transport bounds connection setup and uses aggressive TCP keepalive so a dead
// VPS route is detected while the TUI remains responsive.
func NewClient(dir string) *Client {
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		KeepAliveConfig: net.KeepAliveConfig{
			Enable:   true,
			Idle:     30 * time.Second,
			Interval: 15 * time.Second,
			Count:    4,
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ResponseHeaderTimeout = 2 * time.Minute
	transport.ExpectContinueTimeout = time.Second

	return &Client{
		HTTP:              &http.Client{Transport: transport},
		Dir:               dir,
		StreamIdleTimeout: defaultStreamIdleTimeout,
	}
}

func (c *Client) streamIdleTimeout() time.Duration {
	if c == nil || c.StreamIdleTimeout == 0 {
		return defaultStreamIdleTimeout
	}
	return c.StreamIdleTimeout
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
	// Retry reports a recoverable network interruption. It is a status event,
	// not a terminal error; consumers must keep pumping the same channel.
	Retry *RetryStatus
	Done  bool
	Err   error
}

var errSSEDone = errors.New("sse done")

type responseLine struct {
	line []byte
	err  error
	done bool
}

// scanResponseLines reads the network on its own goroutine and resets the idle
// watchdog for every received line, including SSE keepalive comments. Closing
// the body on timeout/cancellation unblocks the scanner, preventing a half-open
// connection from pinning a provider request indefinitely.
func scanResponseLines(ctx context.Context, body io.ReadCloser, idle time.Duration, consume func([]byte) error) error {
	if body == nil {
		return errors.New("respuesta del proveedor sin body")
	}
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines := make(chan responseLine, 1)
	go func() {
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := bytes.Clone(scanner.Bytes())
			select {
			case lines <- responseLine{line: line}:
			case <-scanCtx.Done():
				return
			}
		}
		result := responseLine{err: scanner.Err(), done: true}
		select {
		case lines <- result:
		case <-scanCtx.Done():
		}
	}()

	var timer *time.Timer
	var timeout <-chan time.Time
	if idle > 0 {
		timer = time.NewTimer(idle)
		timeout = timer.C
		defer timer.Stop()
	}
	resetTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(idle)
	}

	for {
		select {
		case <-ctx.Done():
			cancel()
			_ = body.Close()
			return ctx.Err()
		case <-timeout:
			cancel()
			_ = body.Close()
			return fmt.Errorf("stream sin actividad durante %s", idle.Round(time.Second))
		case result := <-lines:
			if result.done {
				return result.err
			}
			resetTimer()
			if consume != nil {
				if err := consume(result.line); err != nil {
					cancel()
					return err
				}
			}
		}
	}
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

// maxServiceAttempts bounds retries for HTTP 408/429/5xx responses. Pure
// transport interruptions are different: Stream waits until connectivity
// returns or the user cancels the turn.
const maxServiceAttempts = 3

// Stream sends a chat request and pushes chunks into the returned channel.
// Transport interruptions never destroy the active turn. The client probes the
// selected endpoint, waits with bounded exponential backoff and retries the
// exact request when connectivity returns. If an attempt had already emitted a
// partial answer, Retry.Reset tells the TUI to discard that incomplete attempt
// before rendering the replacement stream.
func (c *Client) Stream(ctx context.Context, req Request) <-chan Chunk {
	out := make(chan Chunk, 8)
	go func() {
		defer close(out)
		serviceAttempts := 0
		networkAttempts := 0
		for {
			counter := &countingSink{ctx: ctx, out: out}
			err := c.do(ctx, req, counter)
			if err == nil {
				sendChunk(ctx, out, Chunk{Done: true})
				return
			}
			if ctx.Err() != nil {
				sendChunk(ctx, out, Chunk{Err: ctx.Err()})
				return
			}

			if isNetworkFailure(err) {
				if counter.n > 0 {
					networkAttempts = 0
				}
				networkAttempts++
				if c != nil && c.HTTP != nil {
					c.HTTP.CloseIdleConnections()
				}
				if waitErr := c.waitForConnectivity(ctx, req.Provider, networkAttempts, counter.n > 0, out); waitErr != nil {
					sendChunk(ctx, out, Chunk{Err: waitErr})
					return
				}
				if c != nil && c.HTTP != nil {
					c.HTTP.CloseIdleConnections()
				}
				serviceAttempts = 0
				continue
			}

			if counter.n == 0 && isTransientHTTP(err) && serviceAttempts < maxServiceAttempts-1 {
				networkAttempts = 0
				serviceAttempts++
				delay := time.Duration(serviceAttempts) * time.Second
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					sendChunk(ctx, out, Chunk{Err: ctx.Err()})
					return
				case <-timer.C:
				}
				continue
			}
			sendChunk(ctx, out, Chunk{Err: err})
			return
		}
	}()
	return out
}

// countingSink counts semantic chunks emitted by one provider attempt. Retry
// status chunks bypass it so Reset reflects only incomplete model output.
type countingSink struct {
	ctx context.Context
	out chan<- Chunk
	n   int
}

func (s *countingSink) send(ch Chunk) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.out <- ch:
		s.n++
	case <-ctx.Done():
	}
}

func isTransientHTTP(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"HTTP 500", "HTTP 502", "HTTP 503", "HTTP 504", "HTTP 429", "HTTP 408", "HTTP 520", "HTTP 524"} {
		if strings.Contains(msg, code) {
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

	pending := map[int]*ToolCall{}
	var pendingOrder []int
	var parser reasoningStreamParser
	thinkingActive := false
	structuredSeen := false
	err = scanResponseLines(ctx, resp.Body, c.streamIdleTimeout(), func(line []byte) error {
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			return nil
		}
		payload := bytes.TrimSpace(line[5:])
		if bytes.Equal(payload, []byte("[DONE]")) {
			return errSSEDone
		}
		var raw chatResponse
		if err := json.Unmarshal(payload, &raw); err != nil {
			return nil
		}
		if raw.Error != nil {
			return errors.New(raw.Error.Message)
		}
		if raw.Message != "" && len(raw.Choices) == 0 {
			return errors.New(raw.Message)
		}
		if len(raw.Choices) == 0 {
			return nil
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
		return nil
	})
	if err != nil && !errors.Is(err, errSSEDone) {
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
