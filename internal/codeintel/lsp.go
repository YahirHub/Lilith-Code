package codeintel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const lspResultLimit = 2 << 20

type lspServerSpec struct {
	Name       string
	Executable string
	Args       []string
	Languages  map[string]bool
}

func availableLSPServers(env Environment) []string {
	seen := map[string]bool{}
	var out []string
	for _, spec := range lspSpecs(env) {
		if spec.Executable == "" || seen[spec.Name] {
			continue
		}
		seen[spec.Name] = true
		out = append(out, spec.Name)
	}
	return out
}

func lspSpecs(env Environment) []lspServerSpec {
	tool := func(names ...string) string {
		for _, name := range names {
			if path := env.Tools[name]; path != "" {
				return path
			}
		}
		return ""
	}
	return []lspServerSpec{
		{Name: "gopls", Executable: tool("gopls"), Languages: langSet("go")},
		{Name: "rust-analyzer", Executable: tool("rust-analyzer"), Languages: langSet("rust")},
		{Name: "deno", Executable: tool("deno"), Args: []string{"lsp"}, Languages: langSet("typescript", "javascript", "tsx", "jsx")},
		{Name: "typescript-language-server", Executable: tool("typescript-language-server"), Args: []string{"--stdio"}, Languages: langSet("typescript", "javascript", "tsx", "jsx", "vue", "svelte")},
		{Name: "pyright-langserver", Executable: tool("pyright-langserver"), Args: []string{"--stdio"}, Languages: langSet("python")},
		{Name: "pylsp", Executable: tool("pylsp"), Languages: langSet("python")},
		{Name: "intelephense", Executable: tool("intelephense"), Args: []string{"--stdio"}, Languages: langSet("php")},
		{Name: "clangd", Executable: tool("clangd"), Languages: langSet("c", "cpp", "objective-c")},
		{Name: "jdtls", Executable: tool("jdtls"), Languages: langSet("java")},
		{Name: "kotlin-language-server", Executable: tool("kotlin-language-server"), Languages: langSet("kotlin")},
		{Name: "lua-language-server", Executable: tool("lua-language-server"), Languages: langSet("lua")},
		{Name: "sourcekit-lsp", Executable: tool("sourcekit-lsp"), Languages: langSet("swift")},
		{Name: "ruby-lsp", Executable: tool("ruby-lsp"), Languages: langSet("ruby")},
		{Name: "solargraph", Executable: tool("solargraph"), Args: []string{"stdio"}, Languages: langSet("ruby")},
		{Name: "elixir-ls", Executable: tool("elixir-ls"), Languages: langSet("elixir")},
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func langSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

// Semantic executes a short-lived Language Server Protocol query using an
// already-installed server. Lilith never installs or downloads language
// servers; syntax indexing remains available when no server exists.
func (m *Manager) Semantic(parent context.Context, request SemanticRequest) (SemanticResult, error) {
	operation := strings.ToLower(strings.TrimSpace(request.Operation))
	method, err := semanticMethod(operation)
	if err != nil {
		return SemanticResult{}, err
	}
	path, rel, language, source, err := m.semanticFile(request.Path)
	if err != nil {
		return SemanticResult{}, err
	}
	profile := m.RefreshProfile()
	spec, ok := selectLSP(profile, language)
	if !ok {
		if language == "go" {
			return m.builtinGoSemantic(parent, operation, rel, source, request.Line, request.Column)
		}
		return SemanticResult{}, fmt.Errorf("no installed language server supports %s; syntax intelligence is still available", language)
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	client, err := startLSP(ctx, spec, m.root)
	if err != nil {
		return SemanticResult{}, fmt.Errorf("start %s: %w", spec.Name, err)
	}
	defer client.Close()

	rootURI := fileURI(m.root)
	if _, err := client.Request(ctx, "initialize", map[string]any{
		"processId":        nil,
		"rootUri":          rootURI,
		"workspaceFolders": []map[string]any{{"uri": rootURI, "name": filepath.Base(m.root)}},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"definition":     map[string]any{"linkSupport": true},
				"references":     map[string]any{},
				"hover":          map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"diagnostic":     map[string]any{},
			},
		},
		"clientInfo": map[string]any{"name": "Lilith", "version": "codeintel"},
	}); err != nil {
		return SemanticResult{}, fmt.Errorf("initialize %s: %w", spec.Name, err)
	}
	if err := client.Notify("initialized", map[string]any{}); err != nil {
		return SemanticResult{}, err
	}
	uri := fileURI(path)
	if err := client.Notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": lspLanguageID(language, rel), "version": 1, "text": string(source)},
	}); err != nil {
		return SemanticResult{}, err
	}

	params := semanticParams(operation, uri, request.Line, request.Column)
	result, err := client.Request(ctx, method, params)
	if err != nil && (operation == "diagnostics" || operation == "diagnostic") {
		if published, ok := client.waitPublishedDiagnostics(ctx, uri, 350*time.Millisecond); ok {
			return SemanticResult{Server: spec.Name, Method: "textDocument/publishDiagnostics", Result: published}, nil
		}
	}
	if err != nil {
		return SemanticResult{}, fmt.Errorf("%s %s: %w", spec.Name, method, err)
	}
	return SemanticResult{Server: spec.Name, Method: method, Result: result}, nil
}

func semanticMethod(operation string) (string, error) {
	switch operation {
	case "symbols", "document_symbols", "document-symbols":
		return "textDocument/documentSymbol", nil
	case "definition", "definitions":
		return "textDocument/definition", nil
	case "references", "refs":
		return "textDocument/references", nil
	case "hover", "type", "info":
		return "textDocument/hover", nil
	case "diagnostics", "diagnostic":
		return "textDocument/diagnostic", nil
	default:
		return "", fmt.Errorf("unsupported semantic operation %q (use symbols, definition, references, hover or diagnostics)", operation)
	}
}

func semanticParams(operation, uri string, line, column int) map[string]any {
	if line > 0 {
		line--
	}
	if column > 0 {
		column--
	}
	textDocument := map[string]any{"uri": uri}
	if operation == "symbols" || operation == "document_symbols" || operation == "document-symbols" {
		return map[string]any{"textDocument": textDocument}
	}
	if operation == "diagnostics" || operation == "diagnostic" {
		return map[string]any{"textDocument": textDocument, "identifier": "lilith", "previousResultId": nil}
	}
	params := map[string]any{
		"textDocument": textDocument,
		"position":     map[string]any{"line": maxInt(line, 0), "character": maxInt(column, 0)},
	}
	if operation == "references" || operation == "refs" {
		params["context"] = map[string]any{"includeDeclaration": true}
	}
	return params
}

func (m *Manager) semanticFile(value string) (absolute, relative, language string, source []byte, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", nil, errors.New("semantic query requires a file path")
	}
	absolute = value
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(m.root, value)
	}
	absolute, err = filepath.Abs(absolute)
	if err != nil {
		return "", "", "", nil, err
	}
	relative, err = filepath.Rel(m.root, absolute)
	if pathEscapesRoot(relative, err) {
		return "", "", "", nil, errors.New("semantic path escapes the project root")
	}
	resolved, resolveErr := filepath.EvalSymlinks(absolute)
	if resolveErr != nil {
		return "", "", "", nil, resolveErr
	}
	resolvedRelative, relErr := filepath.Rel(m.root, filepath.Clean(resolved))
	if pathEscapesRoot(resolvedRelative, relErr) {
		return "", "", "", nil, errors.New("semantic path resolves outside the project root")
	}
	absolute = filepath.Clean(resolved)
	relative = resolvedRelative
	info, statErr := os.Stat(absolute)
	if statErr != nil {
		return "", "", "", nil, statErr
	}
	if !info.Mode().IsRegular() {
		return "", "", "", nil, errors.New("semantic path is not a regular file")
	}
	if info.Size() > maxIndexedFileBytes {
		return "", "", "", nil, fmt.Errorf("semantic file exceeds the %d byte limit", maxIndexedFileBytes)
	}
	source, err = os.ReadFile(absolute)
	if err != nil {
		return "", "", "", nil, err
	}
	language = languageForPath(absolute)
	if language == "" {
		return "", "", "", nil, fmt.Errorf("unable to detect language for %s", filepath.ToSlash(relative))
	}
	return absolute, filepath.ToSlash(relative), language, source, nil
}

func selectLSP(profile Profile, language string) (lspServerSpec, bool) {
	specs := lspSpecs(profile.Environment)
	if stringSliceContains(profile.Project.Kinds, "deno") {
		for _, spec := range specs {
			if spec.Name == "deno" && spec.Executable != "" && spec.Languages[language] {
				return spec, true
			}
		}
	}
	for _, spec := range specs {
		if spec.Name != "deno" && spec.Executable != "" && spec.Languages[language] {
			return spec, true
		}
	}
	for _, spec := range specs {
		if spec.Executable != "" && spec.Languages[language] {
			return spec, true
		}
	}
	return lspServerSpec{}, false
}

func lspLanguageID(language, path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case language == "typescript" && ext == ".tsx":
		return "typescriptreact"
	case language == "javascript" && ext == ".jsx":
		return "javascriptreact"
	case language == "cpp":
		return "cpp"
	case language == "csharp":
		return "csharp"
	case language == "shell":
		return "shellscript"
	default:
		return language
	}
}

func fileURI(path string) string {
	absolute, _ := filepath.Abs(path)
	absolute = filepath.ToSlash(absolute)
	if len(absolute) >= 2 && absolute[1] == ':' {
		absolute = "/" + absolute
	}
	return (&url.URL{Scheme: "file", Path: absolute}).String()
}

type lspClient struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	rootURI  string
	rootName string

	requestMu sync.Mutex
	writeMu   sync.Mutex
	nextID    int64
	messages  chan map[string]any
	readErr   chan error
	done      chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once

	diagnosticsMu sync.RWMutex
	diagnostics   map[string]any
}

func startLSP(ctx context.Context, spec lspServerSpec, root string) (*lspClient, error) {
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	var stderr synchronizedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	client := &lspClient{
		cmd:         cmd,
		stdin:       stdin,
		rootURI:     fileURI(root),
		rootName:    filepath.Base(root),
		messages:    make(chan map[string]any, 32),
		readErr:     make(chan error, 1),
		done:        make(chan struct{}),
		diagnostics: map[string]any{},
	}
	go client.readLoop(bufio.NewReader(stdout), &stderr)
	return client, nil
}

func (c *lspClient) readLoop(reader *bufio.Reader, stderr *synchronizedBuffer) {
	for {
		message, err := readLSPMessage(reader)
		if err != nil {
			if detail := strings.TrimSpace(stderr.String()); detail != "" && !errors.Is(err, io.EOF) {
				err = fmt.Errorf("%w: %s", err, truncateLSPDetail(detail))
			}
			select {
			case c.readErr <- err:
			default:
			}
			return
		}
		select {
		case c.messages <- message:
		case <-c.done:
			return
		}
	}
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func truncateLSPDetail(value string) string {
	const limit = 2048
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func (c *lspClient) Close() {
	if c == nil || c.cmd == nil {
		return
	}
	c.closeOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		_, _ = c.Request(shutdownCtx, "shutdown", nil)
		cancel()
		_ = c.Notify("exit", nil)
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		c.stopOnce.Do(func() { close(c.done) })
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
			return
		case <-time.After(750 * time.Millisecond):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			<-done
		}
	})
}

func (c *lspClient) Notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *lspClient) Request(ctx context.Context, method string, params any) (any, error) {
	// Semantic queries are intentionally serialized. This keeps the short-lived
	// client small while the dedicated read loop continues servicing server
	// requests and notifications during each query.
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.nextID++
	id := c.nextID
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-c.readErr:
			return nil, err
		case message := <-c.messages:
			if c.handleIncoming(message) {
				continue
			}
			if responseID(message["id"]) != id {
				continue
			}
			if rawErr, ok := message["error"]; ok && rawErr != nil {
				encoded, _ := json.Marshal(rawErr)
				return nil, errors.New(string(encoded))
			}
			return message["result"], nil
		}
	}
}

func (c *lspClient) handleIncoming(message map[string]any) bool {
	method, _ := message["method"].(string)
	if method == "" {
		return false
	}
	rawID, request := message["id"]
	if !request {
		if method == "textDocument/publishDiagnostics" {
			c.storePublishedDiagnostics(message["params"])
		}
		return true
	}
	result, rpcErr := c.serverRequestResult(method, message["params"])
	response := map[string]any{"jsonrpc": "2.0", "id": rawID}
	if rpcErr != nil {
		response["error"] = rpcErr
	} else {
		response["result"] = result
	}
	_ = c.write(response)
	return true
}

func (c *lspClient) serverRequestResult(method string, params any) (any, map[string]any) {
	switch method {
	case "workspace/configuration":
		count := 0
		if object, ok := params.(map[string]any); ok {
			if items, ok := object["items"].([]any); ok {
				count = len(items)
			}
		}
		return make([]any, count), nil
	case "workspace/workspaceFolders":
		return []map[string]any{{"uri": c.rootURI, "name": c.rootName}}, nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
		return nil, nil
	case "workspace/applyEdit":
		return map[string]any{"applied": false, "failureReason": "Lilith semantic queries are read-only"}, nil
	default:
		return nil, map[string]any{"code": -32601, "message": "Method not found: " + method}
	}
}

func (c *lspClient) storePublishedDiagnostics(params any) {
	object, ok := params.(map[string]any)
	if !ok {
		return
	}
	uri, _ := object["uri"].(string)
	if uri == "" {
		return
	}
	c.diagnosticsMu.Lock()
	c.diagnostics[uri] = object
	c.diagnosticsMu.Unlock()
}

func (c *lspClient) publishedDiagnostics(uri string) (any, bool) {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()
	value, ok := c.diagnostics[uri]
	return value, ok
}

func (c *lspClient) waitPublishedDiagnostics(ctx context.Context, uri string, wait time.Duration) (any, bool) {
	if value, ok := c.publishedDiagnostics(uri); ok {
		return value, true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-timer.C:
			return c.publishedDiagnostics(uri)
		case <-c.readErr:
			return c.publishedDiagnostics(uri)
		case message := <-c.messages:
			c.handleIncoming(message)
			if value, ok := c.publishedDiagnostics(uri); ok {
				return value, true
			}
		}
	}
}

func responseID(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		v, _ := typed.Int64()
		return v
	case string:
		v, _ := strconv.ParseInt(typed, 10, 64)
		return v
	default:
		return 0
	}
}

func (c *lspClient) write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(data))
	if err == nil {
		_, err = c.stdin.Write(data)
	}
	return err
}

func readLSPMessage(reader *bufio.Reader) (map[string]any, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength <= 0 || contentLength > lspResultLimit {
		return nil, fmt.Errorf("invalid LSP Content-Length %d", contentLength)
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var message map[string]any
	if err := decoder.Decode(&message); err != nil {
		return nil, err
	}
	return message, nil
}
