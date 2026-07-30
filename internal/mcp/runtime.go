package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

const protocolVersion = "2025-11-25"

type Tool struct {
	Server      string
	Name        string
	FullName    string
	Description string
	InputSchema any
	ReadOnly    bool
}

type Runtime struct {
	mu      sync.Mutex
	clients map[string]client
	tools   map[string]Tool
}

type client interface {
	Initialize(context.Context) error
	ListTools(context.Context) ([]Tool, error)
	Call(context.Context, string, map[string]any) (string, error)
	Close() error
}

func NewRuntime() *Runtime { return &Runtime{clients: map[string]client{}, tools: map[string]Tool{}} }

func (r *Runtime) Connect(ctx context.Context, configs map[string]ServerConfig) []error {
	if r == nil {
		return []error{errors.New("nil MCP runtime")}
	}
	var errs []error
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		cfg := configs[name]
		c, err := newClient(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %s: %w", name, err))
			continue
		}
		if err = c.Initialize(ctx); err != nil {
			_ = c.Close()
			errs = append(errs, fmt.Errorf("mcp %s initialize: %w", name, err))
			continue
		}
		ts, err := c.ListTools(ctx)
		if err != nil {
			_ = c.Close()
			errs = append(errs, fmt.Errorf("mcp %s tools/list: %w", name, err))
			continue
		}
		r.mu.Lock()
		r.clients[name] = c
		for _, t := range ts {
			t.Server = name
			t.FullName = "mcp__" + sanitize(name) + "__" + sanitize(t.Name)
			r.tools[t.FullName] = t
		}
		r.mu.Unlock()
	}
	return errs
}

func (r *Runtime) Tools() []Tool {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sortTools(out)
	return out
}

func (r *Runtime) Schemas() []any { return r.SchemasForMode(false) }

// SchemasForMode returns MCP tool schemas. When readOnlyOnly is true, only
// tools whose MCP annotations explicitly advertise readOnlyHint are exposed.
// This keeps Plan mode fail-closed for third-party tools.
func (r *Runtime) SchemasForMode(readOnlyOnly bool) []any {
	return schemasForTools(r.Tools(), nil, readOnlyOnly)
}

// SchemasForServers exposes only tools belonging to the named MCP servers.
// An empty server list intentionally means none; callers that want inheritance
// should use SchemasForMode instead. This lets Claude-compatible subagents add
// explicit mcpServers without rebuilding or duplicating the parent runtime.
func (r *Runtime) SchemasForServers(servers []string, readOnlyOnly bool) []any {
	set := map[string]bool{}
	for _, name := range servers {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = true
		}
	}
	return schemasForTools(r.Tools(), set, readOnlyOnly)
}

func schemasForTools(ts []Tool, servers map[string]bool, readOnlyOnly bool) []any {
	out := make([]any, 0, len(ts))
	for _, t := range ts {
		if servers != nil && !servers[t.Server] {
			continue
		}
		if readOnlyOnly && !t.ReadOnly {
			continue
		}
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": t.FullName, "description": t.Description, "parameters": schema}})
	}
	return out
}

// ServerForTool returns the configured server name for a fully-qualified MCP
// tool. It is used by subagent policy code to enforce scoped server access.
func (r *Runtime) ServerForTool(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tools[name]
	return t.Server, ok
}

func (r *Runtime) Has(name string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.tools[name]
	return ok
}
func (r *Runtime) IsReadOnly(name string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tools[name]
	return ok && t.ReadOnly
}
func (r *Runtime) Call(ctx context.Context, fullName string, args map[string]any) (string, error) {
	if r == nil {
		return "", errors.New("MCP unavailable")
	}
	r.mu.Lock()
	t, ok := r.tools[fullName]
	c := r.clients[t.Server]
	r.mu.Unlock()
	if !ok || c == nil {
		return "", fmt.Errorf("unknown MCP tool %s", fullName)
	}
	return c.Call(ctx, t.Name, args)
}

// CallServerTool invokes an MCP tool by the server/tool pair used by Claude
// mcp_tool hooks. Plugin references use plugin:<plugin-name>:<server-name> and
// are translated to the isolated server namespace used by ScopePluginConfigs.
func (r *Runtime) CallServerTool(ctx context.Context, server, tool string, args map[string]any) (string, error) {
	if r == nil {
		return "", errors.New("MCP unavailable")
	}
	server = runtimeServerName(server)
	tool = strings.TrimSpace(tool)
	if server == "" || tool == "" {
		return "", errors.New("MCP server and tool are required")
	}
	r.mu.Lock()
	var selected Tool
	found := false
	for _, candidate := range r.tools {
		if candidate.Server == server && candidate.Name == tool {
			selected = candidate
			found = true
			break
		}
	}
	c := r.clients[server]
	r.mu.Unlock()
	if !found || c == nil {
		return "", fmt.Errorf("unknown MCP tool %s/%s", server, tool)
	}
	return c.Call(ctx, selected.Name, args)
}

func runtimeServerName(server string) string {
	server = strings.TrimSpace(server)
	parts := strings.Split(server, ":")
	if len(parts) >= 3 && strings.EqualFold(parts[0], "plugin") {
		return "plugin_" + sanitizeConfigName(parts[1]) + "_" + sanitizeConfigName(strings.Join(parts[2:], "_"))
	}
	return server
}
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var first error
	for n, c := range r.clients {
		if err := c.Close(); err != nil && first == nil {
			first = fmt.Errorf("%s: %w", n, err)
		}
	}
	r.clients = map[string]client{}
	r.tools = map[string]Tool{}
	return first
}

func newClient(cfg ServerConfig) (client, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "stdio":
		return newStdioClient(cfg), nil
	case "http", "streamable-http", "streamable_http":
		return newHTTPClient(cfg), nil
	case "sse":
		return newSSEClient(cfg), nil
	case "ws", "websocket":
		return newWSClient(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", cfg.Type)
	}
}

// ---- stdio -----------------------------------------------------------------

type stdioClient struct {
	cfg   ServerConfig
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan []byte
	errs  chan error
	mu    sync.Mutex
	seq   atomic.Int64
}

func newStdioClient(cfg ServerConfig) *stdioClient { return &stdioClient{cfg: cfg} }
func (c *stdioClient) start(ctx context.Context) error {
	if c.cmd != nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.Command) == "" {
		return errors.New("stdio command is empty")
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	env := os.Environ()
	for k, v := range c.cfg.Env {
		env = append(env, k+"="+os.ExpandEnv(v))
	}
	cmd.Env = env
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return err
	}
	c.cmd = cmd
	c.stdin = in
	c.lines = make(chan []byte, 32)
	c.errs = make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(out)
		buf := make([]byte, 64*1024)
		sc.Buffer(buf, 4*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			c.lines <- line
		}
		if e := sc.Err(); e != nil {
			c.errs <- e
		}
		close(c.lines)
	}()
	return nil
}
func (c *stdioClient) rpc(ctx context.Context, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	id := c.seq.Add(1)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-c.errs:
			if err != nil {
				return nil, err
			}
		case line, ok := <-c.lines:
			if !ok {
				return nil, io.EOF
			}
			var raw map[string]any
			if json.Unmarshal(line, &raw) != nil {
				continue
			}
			rid, has := raw["id"]
			if !has || fmt.Sprint(rid) != fmt.Sprint(id) {
				continue
			}
			if e, ok := raw["error"].(map[string]any); ok {
				return nil, fmt.Errorf("MCP error: %v", e["message"])
			}
			res, _ := raw["result"].(map[string]any)
			return res, nil
		}
	}
}
func (c *stdioClient) notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(ctx); err != nil {
		return err
	}
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	_, err := c.stdin.Write(append(data, '\n'))
	return err
}
func (c *stdioClient) Initialize(ctx context.Context) error {
	res, err := c.rpc(ctx, "initialize", initializeParams())
	if err != nil {
		return err
	}
	_ = res
	return c.notify(ctx, "notifications/initialized", nil)
}
func (c *stdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	return listToolsPaged(ctx, c.rpc)
}
func (c *stdioClient) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	return formatCallResult(res), nil
}
func (c *stdioClient) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	return nil
}

// ---- Streamable HTTP --------------------------------------------------------
type httpClient struct {
	cfg         ServerConfig
	http        *http.Client
	mu          sync.Mutex
	seq         atomic.Int64
	session     string
	initialized bool
}

func newHTTPClient(cfg ServerConfig) *httpClient {
	return &httpClient{cfg: cfg, http: &http.Client{Timeout: 5 * time.Minute}}
}
func (c *httpClient) rpc(ctx context.Context, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.seq.Add(1)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, os.ExpandEnv(c.cfg.URL), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.initialized {
		req.Header.Set("MCP-Protocol-Version", protocolVersion)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	var raw map[string]any
	if strings.Contains(ct, "text/event-stream") {
		raw, err = readSSEResponse(resp.Body, id)
	} else {
		err = json.NewDecoder(resp.Body).Decode(&raw)
	}
	if err != nil {
		return nil, err
	}
	if e, ok := raw["error"].(map[string]any); ok {
		return nil, fmt.Errorf("MCP error: %v", e["message"])
	}
	res, _ := raw["result"].(map[string]any)
	return res, nil
}
func (c *httpClient) notify(ctx context.Context, method string, params any) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, os.ExpandEnv(c.cfg.URL), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func (c *httpClient) Initialize(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.URL) == "" {
		return errors.New("http MCP url is empty")
	}
	if _, err := c.rpc(ctx, "initialize", initializeParams()); err != nil {
		return err
	}
	c.initialized = true
	return c.notify(ctx, "notifications/initialized", nil)
}
func (c *httpClient) ListTools(ctx context.Context) ([]Tool, error) {
	return listToolsPaged(ctx, c.rpc)
}
func (c *httpClient) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	return formatCallResult(res), nil
}
func (c *httpClient) Close() error { return nil }

// ---- Legacy SSE ------------------------------------------------------------
// Claude still accepts the pre-Streamable-HTTP MCP SSE transport. The server
// exposes a long-lived event stream whose `endpoint` event tells the client
// where JSON-RPC messages must be POSTed.
type sseClient struct {
	cfg      ServerConfig
	http     *http.Client
	mu       sync.Mutex
	seq      atomic.Int64
	endpoint string
	events   chan map[string]any
	errs     chan error
	cancel   context.CancelFunc
}

func newSSEClient(cfg ServerConfig) *sseClient {
	return &sseClient{cfg: cfg, http: &http.Client{Timeout: 0}}
}
func (c *sseClient) start(ctx context.Context) error {
	if c.events != nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.URL) == "" {
		return errors.New("sse MCP url is empty")
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, os.ExpandEnv(c.cfg.URL), nil)
	if err != nil {
		cancel()
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		cancel()
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	c.events = make(chan map[string]any, 32)
	c.errs = make(chan error, 1)
	c.cancel = cancel
	endpointReady := make(chan struct{}, 1)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		eventType := ""
		var data []string
		flush := func() {
			payload := strings.TrimSpace(strings.Join(data, "\n"))
			if eventType == "endpoint" && payload != "" {
				base, _ := url.Parse(c.cfg.URL)
				ref, err := url.Parse(payload)
				if err == nil && base != nil {
					c.endpoint = base.ResolveReference(ref).String()
					select {
					case endpointReady <- struct{}{}:
					default:
					}
				}
			} else if payload != "" {
				var raw map[string]any
				if json.Unmarshal([]byte(payload), &raw) == nil {
					c.events <- raw
				}
			}
			eventType = ""
			data = nil
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		flush()
		if err := sc.Err(); err != nil {
			select {
			case c.errs <- err:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-endpointReady:
		return nil
	case err := <-c.errs:
		return err
	case <-time.After(10 * time.Second):
		if strings.TrimSpace(c.endpoint) == "" {
			return errors.New("SSE MCP server did not provide endpoint event")
		}
		return nil
	}
}
func (c *sseClient) send(ctx context.Context, msg map[string]any) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	data, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func (c *sseClient) rpc(ctx context.Context, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.seq.Add(1)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.send(ctx, msg); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-c.errs:
			if err != nil {
				return nil, err
			}
		case raw := <-c.events:
			if fmt.Sprint(raw["id"]) != fmt.Sprint(id) {
				continue
			}
			if e, ok := raw["error"].(map[string]any); ok {
				return nil, fmt.Errorf("MCP error: %v", e["message"])
			}
			res, _ := raw["result"].(map[string]any)
			return res, nil
		}
	}
}
func (c *sseClient) notify(ctx context.Context, method string, params any) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.send(ctx, msg)
}
func (c *sseClient) Initialize(ctx context.Context) error {
	if _, err := c.rpc(ctx, "initialize", initializeParams()); err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized", nil)
}
func (c *sseClient) ListTools(ctx context.Context) ([]Tool, error) { return listToolsPaged(ctx, c.rpc) }
func (c *sseClient) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	return formatCallResult(res), nil
}
func (c *sseClient) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

// ---- WebSocket -------------------------------------------------------------
type wsClient struct {
	cfg  ServerConfig
	mu   sync.Mutex
	seq  atomic.Int64
	conn *websocket.Conn
}

func newWSClient(cfg ServerConfig) *wsClient { return &wsClient{cfg: cfg} }
func (c *wsClient) start(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.URL) == "" {
		return errors.New("websocket MCP url is empty")
	}
	origin := "http://localhost/"
	cfg, err := websocket.NewConfig(os.ExpandEnv(c.cfg.URL), origin)
	if err != nil {
		return err
	}
	for k, v := range c.cfg.Headers {
		cfg.Header.Set(k, os.ExpandEnv(v))
	}
	type result struct {
		conn *websocket.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() { conn, err := websocket.DialConfig(cfg); ch <- result{conn, err} }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		c.conn = r.conn
		return nil
	}
}
func (c *wsClient) rpc(ctx context.Context, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(ctx); err != nil {
		return nil, err
	}
	id := c.seq.Add(1)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	if err := websocket.Message.Send(c.conn, string(data)); err != nil {
		return nil, err
	}
	for {
		type recv struct {
			s   string
			err error
		}
		ch := make(chan recv, 1)
		go func() { var v string; e := websocket.Message.Receive(c.conn, &v); ch <- recv{v, e} }()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return nil, r.err
			}
			var raw map[string]any
			if json.Unmarshal([]byte(r.s), &raw) != nil || fmt.Sprint(raw["id"]) != fmt.Sprint(id) {
				continue
			}
			if e, ok := raw["error"].(map[string]any); ok {
				return nil, fmt.Errorf("MCP error: %v", e["message"])
			}
			res, _ := raw["result"].(map[string]any)
			return res, nil
		}
	}
}
func (c *wsClient) notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.start(ctx); err != nil {
		return err
	}
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	return websocket.Message.Send(c.conn, string(data))
}
func (c *wsClient) Initialize(ctx context.Context) error {
	if _, err := c.rpc(ctx, "initialize", initializeParams()); err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized", nil)
}
func (c *wsClient) ListTools(ctx context.Context) ([]Tool, error) { return listToolsPaged(ctx, c.rpc) }
func (c *wsClient) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	res, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	return formatCallResult(res), nil
}
func (c *wsClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func initializeParams() map[string]any {
	return map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "lilith", "version": "1"}}
}

type rpcFunc func(context.Context, string, any) (map[string]any, error)

func listToolsPaged(ctx context.Context, rpc rpcFunc) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, err := rpc(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		arr, _ := res["tools"].([]any)
		for _, v := range arr {
			m, _ := v.(map[string]any)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			desc, _ := m["description"].(string)
			readOnly := false
			if annotations, ok := m["annotations"].(map[string]any); ok {
				readOnly, _ = annotations["readOnlyHint"].(bool)
			}
			out = append(out, Tool{Name: name, Description: desc, InputSchema: m["inputSchema"], ReadOnly: readOnly})
		}
		cursor, _ = res["nextCursor"].(string)
		if cursor == "" {
			break
		}
	}
	return out, nil
}
func formatCallResult(res map[string]any) string {
	if res == nil {
		return ""
	}
	var parts []string
	if arr, ok := res["content"].([]any); ok {
		for _, v := range arr {
			m, _ := v.(map[string]any)
			if text, _ := m["text"].(string); text != "" {
				parts = append(parts, text)
				continue
			}
			b, _ := json.Marshal(m)
			parts = append(parts, string(b))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	b, _ := json.Marshal(res)
	return string(b)
}
func readSSEResponse(r io.Reader, id int64) (map[string]any, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var raw map[string]any
		if json.Unmarshal([]byte(data), &raw) != nil {
			continue
		}
		rid, ok := raw["id"]
		if ok && fmt.Sprint(rid) == fmt.Sprint(id) {
			return raw, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
func sortTools(v []Tool) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].FullName < v[j-1].FullName; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
