package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyWriteExistingFileIsInterceptedWithoutMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "styles.css")
	before := []byte("body { color: red; }\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Execute(context.Background(), "write", map[string]any{
		"path":    "styles.css",
		"content": strings.Repeat(".huge { display:block; }\n", 1000),
	}, Env{Root: root})
	if err != nil {
		t.Fatalf("write should be a recoverable interception: %v", err)
	}
	if !strings.HasPrefix(out, "FILE_EXISTS:") || !strings.Contains(out, "write_file") {
		t.Fatalf("write returned a non-actionable interception: %q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatalf("write mutated an existing file: %q", got)
	}
}

func TestLegacyWriteMissingFileRedirectsToCreateFileWithoutCreating(t *testing.T) {
	root := t.TempDir()
	out, err := Execute(context.Background(), "write", map[string]any{
		"path": "new.txt", "content": "should not be written",
	}, Env{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "USE_CREATE_FILE:") {
		t.Fatalf("unexpected result: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("legacy write alias must never create a file; stat err=%v", err)
	}
}

func TestOnlyAmbiguousLegacyWriteNameIsHidden(t *testing.T) {
	joined := strings.Join(Names(), ",")
	if !strings.Contains(joined, "write_file") || !strings.Contains(joined, "append_file") {
		t.Fatalf("native file writers must be exposed: %s", joined)
	}
	if strings.Contains(joined, ",write,") || strings.HasPrefix(joined, "write,") || strings.HasSuffix(joined, ",write") {
		t.Fatalf("ambiguous legacy write must stay hidden: %s", joined)
	}
}

func TestCreateFileSchemaSerializesPathBeforeContent(t *testing.T) {
	schemas := Schemas([]string{"create_file"})
	if len(schemas) != 1 {
		t.Fatalf("expected create_file schema, got %d", len(schemas))
	}
	data, err := json.Marshal(schemas[0].Function.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	pathAt := strings.Index(text, `"path"`)
	contentAt := strings.Index(text, `"content"`)
	if pathAt < 0 || contentAt < 0 || pathAt >= contentAt {
		t.Fatalf("path must be advertised before content for early streaming preflight: %s", text)
	}
}

func TestToolSearchMaterializesNativeWriterButNotCreateOnlyToolForGenericWrite(t *testing.T) {
	var added []string
	out, err := Execute(context.Background(), "tool_search", map[string]any{"query": "write file"}, Env{
		Root:        t.TempDir(),
		Materialize: func(names []string) { added = append(added, names...) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(added, "write_file") {
		t.Fatalf("generic write query should enable write_file: %v\n%s", added, out)
	}
	if containsName(added, "create_file") {
		t.Fatalf("generic write query must not enable create_file: %v\n%s", added, out)
	}
}

func TestToolSearchMaterializesCreateFileForExplicitCreationQuery(t *testing.T) {
	var added []string
	_, err := Execute(context.Background(), "tool_search", map[string]any{"query": "create new file"}, Env{
		Root:        t.TempDir(),
		Materialize: func(names []string) { added = append(added, names...) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(added, "create_file") {
		t.Fatalf("explicit creation query should enable create_file: %v", added)
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
