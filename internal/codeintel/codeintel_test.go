package codeintel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func writeTestFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newGoFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "service/service.go", `package service

import "fmt"

type Runner interface { Run() error }

type Service struct{}

func (Service) Run() error {
	fmt.Println("running")
	return helper()
}

func helper() error { return nil }
`)
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	return manager, root
}

func TestDetectProfileAndPersistentIndex(t *testing.T) {
	manager, _ := newGoFixture(t)
	profile := manager.Profile()
	if profile.Project.PrimaryLanguage != "go" {
		t.Fatalf("primary language = %q, want go", profile.Project.PrimaryLanguage)
	}
	if !contains(profile.Project.Kinds, "go") || !contains(profile.Adapters, "go") {
		t.Fatalf("profile missing Go detection: %#v", profile)
	}

	stats, err := manager.EnsureFresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files < 1 || stats.Symbols < 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if _, err := os.Stat(manager.cachePath); err != nil {
		t.Fatalf("persistent index was not written: %v", err)
	}

	reloaded := New(manager.root, manager.configDir)
	idx := reloaded.Snapshot()
	if len(idx.Files) == 0 {
		t.Fatal("reloaded index is empty")
	}
}

func TestSymbolsContextReferencesAndGraph(t *testing.T) {
	manager, _ := newGoFixture(t)
	ctx := context.Background()
	symbols, err := manager.Symbols(ctx, "helper", "service", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) == 0 {
		t.Fatal("helper symbol not found")
	}
	chunks, err := manager.Context(ctx, "helper", nil, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 || !strings.Contains(chunks[0].Content, "helper") {
		t.Fatalf("context did not include helper: %#v", chunks)
	}
	refs, defs, err := manager.References(ctx, "helper", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs)+len(defs) == 0 {
		t.Fatal("helper produced no references or definitions")
	}
	graph, err := manager.Graph(ctx, "service", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("repository graph is empty")
	}
}

func TestIncrementalDeletionAndCancellationAreTransactional(t *testing.T) {
	manager, root := newGoFixture(t)
	if _, err := manager.EnsureFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	old := before.Files["service/service.go"]
	writeTestFile(t, root, "service/service.go", "package service\n\nfunc Changed() {}\n")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.EnsureFresh(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled refresh error = %v", err)
	}
	afterCancel := manager.Snapshot().Files["service/service.go"]
	if afterCancel.SHA256 != old.SHA256 {
		t.Fatal("cancelled refresh partially changed the live index")
	}

	if _, err := manager.EnsureFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated := manager.Snapshot().Files["service/service.go"]
	if updated.SHA256 == old.SHA256 {
		t.Fatal("modified file was not re-indexed")
	}
	if err := os.Remove(filepath.Join(root, "service", "service.go")); err != nil {
		t.Fatal(err)
	}
	stats, err := manager.EnsureFresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Fatalf("removed = %d, want 1", stats.Removed)
	}
	if _, ok := manager.Snapshot().Files["service/service.go"]; ok {
		t.Fatal("deleted file remains in index")
	}
}

func TestNormalizeChangedPathsRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	inside := writeTestFile(t, root, "src/main.go", "package main\n")
	outside := writeTestFile(t, t.TempDir(), "outside.go", "package outside\n")
	got := normalizeChangedPaths(root, []string{"src/main.go", inside, "../escape.go", outside, "../../other"})
	if len(got) != 1 || got[0] != filepath.Join("src", "main.go") {
		t.Fatalf("normalized paths = %#v", got)
	}
}

func TestNormalizeChangedPathsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	outside := writeTestFile(t, outsideRoot, "outside.go", "package outside\n")
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := normalizeChangedPaths(root, []string{"linked.go"}); len(got) != 0 {
		t.Fatalf("symlink escape accepted: %#v", got)
	}
}

func TestSemanticFileRejectsProjectEscape(t *testing.T) {
	manager, _ := newGoFixture(t)
	outside := writeTestFile(t, t.TempDir(), "outside.go", "package outside\n")
	if _, _, _, _, err := manager.semanticFile(outside); err == nil {
		t.Fatal("semanticFile accepted a path outside the project")
	}
}

func TestReadLSPMessage(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":7,"result":{"name":"ok"}}`)
	framed := append([]byte("Content-Length: "+itoa(len(payload))+"\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n"), payload...)
	message, err := readLSPMessage(bufio.NewReader(bytes.NewReader(framed)))
	if err != nil {
		t.Fatal(err)
	}
	if responseID(message["id"]) != 7 {
		t.Fatalf("response id = %#v", message["id"])
	}
	encoded, _ := json.Marshal(message["result"])
	if !strings.Contains(string(encoded), "ok") {
		t.Fatalf("unexpected result: %s", encoded)
	}
}

func TestParseGitPorcelainZHandlesRenameAndSpaces(t *testing.T) {
	parsed := parseGitPorcelainZ([]byte(" M file with spaces.go\x00R  new/name.go\x00old/name.go\x00"))
	for _, path := range []string{"file with spaces.go", "new/name.go", "old/name.go"} {
		if !parsed[path] {
			t.Fatalf("missing changed path %q in %#v", path, parsed)
		}
	}
}

func TestLSPServerRequestsAndPublishedDiagnostics(t *testing.T) {
	client := &lspClient{rootURI: "file:///tmp/project", rootName: "project", diagnostics: map[string]any{}}
	result, rpcErr := client.serverRequestResult("workspace/configuration", map[string]any{"items": []any{map[string]any{}, map[string]any{}}})
	if rpcErr != nil || len(result.([]any)) != 2 {
		t.Fatalf("workspace/configuration result=%#v error=%#v", result, rpcErr)
	}
	client.handleIncoming(map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params":  map[string]any{"uri": "file:///tmp/project/main.go", "diagnostics": []any{}},
	})
	if _, ok := client.publishedDiagnostics("file:///tmp/project/main.go"); !ok {
		t.Fatal("published diagnostics were not retained")
	}
}

func TestPythonValidationUsesNonWritingASTParser(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "pyproject.toml", "[project]\nname='demo'\n")
	writeTestFile(t, root, "main.py", "value = 1\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	profile := manager.Profile()
	profile.Environment.Tools["python3"] = "python3"
	commands := manager.validationCommands(profile, ValidationOptions{ChangedPaths: []string{"main.py"}}, "quick")
	found := false
	for _, command := range commands {
		if command.name != "python AST syntax check" {
			continue
		}
		found = true
		joined := strings.Join(command.args, " ")
		if strings.Contains(joined, "compileall") {
			t.Fatalf("Python validation still writes bytecode: %#v", command)
		}
	}
	if !found {
		t.Fatal("Python AST validation command was not selected")
	}
}

func TestFindSCIPIndex(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".scip/index.scip", "fixture")
	if got := findSCIPIndex(root); got != ".scip/index.scip" {
		t.Fatalf("SCIP index = %q", got)
	}
}

func TestFormatValidationDoesNotFormatRustWithoutExplicitPaths(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "Cargo.toml", "[package]\nname='demo'\nversion='0.1.0'\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	profile := manager.Profile()
	profile.Environment.Tools["cargo"] = filepath.Join(root, "cargo")
	commands := manager.validationCommands(profile, ValidationOptions{ApplyFormat: true}, "quick")
	for _, command := range commands {
		if strings.Contains(command.name, "fmt apply") {
			t.Fatalf("unexpected repository-wide formatter: %#v", command)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buf [32]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}

func TestDetectFrameworksDotnetAndExtendedAdapters(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "package.json", `{"dependencies":{"next":"latest","react":"latest"}}`)
	writeTestFile(t, root, "Demo.csproj", `<Project Sdk="Microsoft.NET.Sdk"></Project>`)
	writeTestFile(t, root, "pubspec.yaml", "name: demo\n")
	project := detectProject(root)
	for _, kind := range []string{"node", "dotnet", "dart"} {
		if !stringSliceContains(project.Kinds, kind) {
			t.Fatalf("missing project kind %q in %#v", kind, project.Kinds)
		}
	}
	for _, framework := range []string{"nextjs", "react"} {
		if !stringSliceContains(project.Frameworks, framework) {
			t.Fatalf("missing framework %q in %#v", framework, project.Frameworks)
		}
	}
	for _, adapter := range []string{"node", "dotnet", "dart"} {
		if !stringSliceContains(availableAdapterNames(project), adapter) {
			t.Fatalf("missing adapter %q", adapter)
		}
	}
}

func TestFindSCIPIndexRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := writeTestFile(t, t.TempDir(), "index.scip", "outside")
	if err := os.Symlink(outside, filepath.Join(root, "index.scip")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := findSCIPIndex(root); got != "" {
		t.Fatalf("SCIP symlink escape accepted: %q", got)
	}
}

func TestSemanticFileRejectsSymlinkEscapeAndLargeFile(t *testing.T) {
	manager, root := newGoFixture(t)
	outside := writeTestFile(t, t.TempDir(), "outside.go", "package outside\n")
	link := filepath.Join(root, "outside-link.go")
	if err := os.Symlink(outside, link); err == nil {
		if _, _, _, _, err := manager.semanticFile("outside-link.go"); err == nil {
			t.Fatal("semanticFile accepted a symlink outside the project")
		}
	}
	large := filepath.Join(root, "large.go")
	if err := os.WriteFile(large, bytes.Repeat([]byte{'x'}, maxIndexedFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := manager.semanticFile("large.go"); err == nil {
		t.Fatal("semanticFile accepted an oversized file")
	}
}

func TestDenoAdapterUsesConcreteFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "deno.json", `{}`)
	writeTestFile(t, root, "src/main.ts", "export const value = 1;\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	profile := manager.Profile()
	profile.Environment.Tools["deno"] = filepath.Join(root, "deno")
	commands := manager.validationCommands(profile, ValidationOptions{}, "quick")
	for _, command := range commands {
		if command.name != "deno check" {
			continue
		}
		joined := strings.Join(command.args, " ")
		if strings.Contains(joined, "**") || !strings.Contains(joined, filepath.Join("src", "main.ts")) {
			t.Fatalf("deno check did not use concrete source paths: %#v", command.args)
		}
		return
	}
	t.Fatal("deno check command was not selected")
}

func TestDotnetValidationDoesNotRequireFormatterUnlessRequested(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "Demo.csproj", `<Project Sdk="Microsoft.NET.Sdk"></Project>`)
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	profile := manager.Profile()
	profile.Environment.Tools["dotnet"] = filepath.Join(root, "dotnet")

	commands := manager.validationCommands(profile, ValidationOptions{}, "quick")
	foundBuild := false
	for _, command := range commands {
		if strings.Contains(command.name, "format") {
			t.Fatalf("ordinary validation unexpectedly requires dotnet format: %#v", command)
		}
		if command.name == "dotnet build" {
			foundBuild = true
		}
	}
	if !foundBuild {
		t.Fatal("dotnet build validation was not selected")
	}

	commands = manager.validationCommands(profile, ValidationOptions{ApplyFormat: true}, "quick")
	foundFormat := false
	for _, command := range commands {
		if command.name == "dotnet format apply" {
			foundFormat = true
		}
	}
	if !foundFormat {
		t.Fatal("explicit format validation did not select dotnet format")
	}
}

type testTokenSource struct{}

func (testTokenSource) Next() gotreesitter.Token {
	return gotreesitter.Token{}
}

func TestAdaptTokenSourceFactory(t *testing.T) {
	language := &gotreesitter.Language{}
	expected := testTokenSource{}
	called := false
	entry := &grammars.LangEntry{
		Name: "fixture",
		TokenSourceFactory: func(source []byte, receivedLanguage *gotreesitter.Language) gotreesitter.TokenSource {
			called = true
			if string(source) != "package fixture" {
				t.Fatalf("source = %q", source)
			}
			if receivedLanguage != language {
				t.Fatal("language was not forwarded to the grammar token source factory")
			}
			return expected
		},
	}

	tokenSource, err := adaptTokenSourceFactory(entry, language)([]byte("package fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("grammar token source factory was not called")
	}
	if _, ok := tokenSource.(testTokenSource); !ok {
		t.Fatalf("token source type = %T", tokenSource)
	}
}

func TestAdaptTokenSourceFactoryRejectsNil(t *testing.T) {
	entry := &grammars.LangEntry{
		Name: "fixture",
		TokenSourceFactory: func([]byte, *gotreesitter.Language) gotreesitter.TokenSource {
			return nil
		},
	}

	if _, err := adaptTokenSourceFactory(entry, &gotreesitter.Language{})(nil); err == nil {
		t.Fatal("nil token source was accepted")
	}
}

func TestGoConstantsAndQualifiedReferences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "internal/version/version.go", `package version

const Current = "0.2.0"
`)
	writeTestFile(t, root, "internal/other/other.go", `package other

type state struct{}
func (state) current() string { return "wrong" }
`)
	writeTestFile(t, root, "cmd/app/main.go", `package main

import buildversion "example.com/demo/internal/version"

func main() { println(buildversion.Current) }
`)
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	ctx := context.Background()

	symbols, err := manager.Symbols(ctx, "Current", "internal/version", "constant", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].Kind != "constant" || symbols[0].QualifiedName != "example.com/demo/internal/version.Current" {
		t.Fatalf("constant symbols = %#v", symbols)
	}
	refs, defs, err := manager.References(ctx, "version.Current", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Path != "internal/version/version.go" {
		t.Fatalf("qualified definitions = %#v", defs)
	}
	if len(refs) != 1 || refs[0].Path != "cmd/app/main.go" || refs[0].Receiver != "buildversion" {
		t.Fatalf("qualified references = %#v", refs)
	}
	for _, ref := range refs {
		if ref.Name == "current" || ref.Path == "internal/other/other.go" {
			t.Fatalf("lowercase false positive leaked into references: %#v", refs)
		}
	}
}

func TestTaskContextPrefersNetworkRecoveryCode(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "internal/providers/openai/retry.go", `package openai

func isNetworkFailure(err error) bool { return err != nil }
func waitForConnectivity() {}
func retryDelay() {}
`)
	writeTestFile(t, root, "internal/providers/openai/client.go", `package openai

func Stream() { waitForConnectivity(); retryDelay() }
`)
	writeTestFile(t, root, "contexto/notes.md", "detectar errores temporales de red esperar internet reintentar proveedor evitar duplicar respuesta parcial\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	chunks, err := manager.Context(context.Background(), "detectar errores temporales de red; esperar a que vuelva Internet; reintentar una solicitud del proveedor; evitar duplicar una respuesta parcial", nil, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("network recovery context is empty")
	}
	foundRetry := false
	for i, chunk := range chunks {
		if strings.HasSuffix(chunk.Path, ".md") {
			t.Fatalf("documentation outranked implementation at position %d: %#v", i, chunks)
		}
		if chunk.Path == "internal/providers/openai/retry.go" {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatalf("retry.go missing from task context: %#v", chunks)
	}
}

func TestGraphExpandsMessageFlowAcrossImportedPackage(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "internal/providers/openai/client.go", `package openai

func Stream() {}
`)
	writeTestFile(t, root, "internal/tui/chat.go", `package tui

import "example.com/demo/internal/providers/openai"

func sendMessage() { openai.Stream() }
`)
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	graph, err := manager.Graph(context.Background(), "envío de mensaje en la TUI hasta la llamada al proveedor OpenAI", 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("flow graph is empty: %#v", graph)
	}
	paths := map[string]bool{}
	for _, node := range graph.Nodes {
		paths[node.Path] = true
	}
	if !paths["internal/tui/chat.go"] || !paths["internal/providers/openai/client.go"] {
		t.Fatalf("flow endpoints missing from graph nodes: %#v", graph.Nodes)
	}
	foundCall := false
	for _, edge := range graph.Edges {
		if edge.Kind == "calls" {
			foundCall = true
			break
		}
	}
	if !foundCall {
		t.Fatalf("flow graph has no call edge: %#v", graph.Edges)
	}
}

func TestStatusTextExposesPhysicalIndexPath(t *testing.T) {
	manager, _ := newGoFixture(t)
	status, err := manager.StatusText(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "index_path: "+manager.cachePath) {
		t.Fatalf("status does not expose cache path: %s", status)
	}
}

func TestWindowsSkipsSecondaryPOSIXMakeAdapter(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "Makefile", "build:\n\tCGO_ENABLED=0 go build ./cmd/app\n\tmkdir -p dist\n")
	project := detectProject(root)
	profile := Profile{Environment: Environment{OS: "windows", Tools: map[string]string{"go": "go.exe", "make": "make.exe"}}, Project: project}
	names := availableAdapterNamesFor(profile, root)
	if !stringSliceContains(names, "go") {
		t.Fatalf("Go adapter missing: %#v", names)
	}
	if stringSliceContains(names, "make") {
		t.Fatalf("POSIX Make adapter should be disabled on native Windows: %#v", names)
	}
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	commands := manager.validationCommands(profile, ValidationOptions{}, "full")
	for _, command := range commands {
		if command.name == "make" {
			t.Fatalf("Windows validation selected incompatible make command: %#v", commands)
		}
	}
}

func TestSourceCommandNamedBuildIsIndexedButGeneratedRootBuildIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "cmd/build/main.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, root, "build/generated.go", "package generated\n\nfunc Generated() {}\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	if _, err := manager.EnsureFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	index := manager.Snapshot()
	if _, ok := index.Files["cmd/build/main.go"]; !ok {
		t.Fatal("source command cmd/build was incorrectly ignored")
	}
	if _, ok := index.Files["build/generated.go"]; ok {
		t.Fatal("generated root build directory was indexed")
	}
}

func TestBuiltinGoSemanticWorksWithoutGopls(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "internal/version/version.go", `package version

const Current = "0.2.0"
`)
	writeTestFile(t, root, "cmd/app/main.go", `package main

import buildversion "example.com/demo/internal/version"

func main() { println(buildversion.Current) }
`)
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	ctx := context.Background()

	result, err := manager.builtinGoSemantic(ctx, "definition", "cmd/app/main.go", []byte(`package main

import buildversion "example.com/demo/internal/version"

func main() { println(buildversion.Current) }
`), 5, 36)
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "builtin-go" {
		t.Fatalf("server=%q; want builtin-go", result.Server)
	}
	payload, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("definition result type=%T", result.Result)
	}
	defs, ok := payload["definitions"].([]Symbol)
	if !ok || len(defs) != 1 || defs[0].QualifiedName != "example.com/demo/internal/version.Current" {
		t.Fatalf("definitions=%#v", payload["definitions"])
	}

	hover, err := manager.builtinGoSemantic(ctx, "hover", "internal/version/version.go", []byte("package version\n\nconst Current = \"0.2.0\"\n"), 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	hoverPayload := hover.Result.(map[string]any)
	if !strings.Contains(hoverPayload["scope"].(string), "static syntax fallback") {
		t.Fatalf("hover scope=%#v", hoverPayload)
	}

	refHover, err := manager.builtinGoSemantic(ctx, "hover", "cmd/app/main.go", []byte(`package main

import buildversion "example.com/demo/internal/version"

func main() { println(buildversion.Current) }
`), 5, 36)
	if err != nil {
		t.Fatal(err)
	}
	refPayload := refHover.Result.(map[string]any)
	if got := refPayload["source_line"]; got != `const Current = "0.2.0"` {
		t.Fatalf("imported hover declaration line=%#v", got)
	}
	if complete, _ := refPayload["type_complete"].(bool); complete {
		t.Fatal("static fallback incorrectly claimed compiler-complete types")
	}
}

func TestBuiltinGoSemanticDiagnostics(t *testing.T) {
	manager, _ := newGoFixture(t)
	result, err := manager.builtinGoSemantic(context.Background(), "diagnostics", "service/service.go", []byte("package service\nfunc broken( {\n"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, ok := result.Result.([]goDiagnostic)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("diagnostics=%#v", result.Result)
	}
	if diagnostics[0].Line <= 0 || diagnostics[0].Message == "" {
		t.Fatalf("invalid diagnostic=%#v", diagnostics[0])
	}
}

func TestGraphDoesNotFanOutAmbiguousSameNameCalls(t *testing.T) {
	record := FileRecord{Package: "caller", PackagePath: "example.com/demo/caller"}
	ref := Reference{Name: "contains", Kind: "call", Receiver: "rect"}
	candidates := []Symbol{
		{Name: "contains", Kind: "method", Package: "one", QualifiedName: "example.com/demo/one.Rect.contains"},
		{Name: "contains", Kind: "method", Package: "two", QualifiedName: "example.com/demo/two.Box.contains"},
	}
	if targets := graphCallTargets(record, ref, candidates); len(targets) != 0 {
		t.Fatalf("ambiguous cross-package call produced false targets: %#v", targets)
	}
}

func TestGraphResolvesImportedPackageCallOnly(t *testing.T) {
	record := FileRecord{
		Package:       "caller",
		PackagePath:   "example.com/demo/caller",
		ImportAliases: map[string]string{"openai": "example.com/demo/openai"},
	}
	ref := Reference{Name: "Stream", Kind: "call", Receiver: "openai"}
	candidates := []Symbol{
		{Name: "Stream", Kind: "function", QualifiedName: "example.com/demo/openai.Stream"},
		{Name: "Stream", Kind: "function", QualifiedName: "example.com/demo/other.Stream"},
	}
	targets := graphCallTargets(record, ref, candidates)
	if len(targets) != 1 || targets[0].QualifiedName != "example.com/demo/openai.Stream" {
		t.Fatalf("import-qualified targets=%#v", targets)
	}
}

func TestSemanticUsesBuiltinGoFallbackWithoutGopls(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.24\n")
	writeTestFile(t, root, "version/version.go", "package version\n\nconst Current = \"0.2.0\"\n")
	manager := New(root, filepath.Join(t.TempDir(), "config"))
	result, err := manager.Semantic(context.Background(), SemanticRequest{
		Operation: "definition",
		Path:      "version/version.go",
		Line:      3,
		Column:    8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "builtin-go" {
		t.Fatalf("server=%q; want builtin-go", result.Server)
	}
}
