package textsearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchLiteralGlobContextAndLimit(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(name, body string) {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a.go", "one\nNeedle\nthree\nneedle\n")
	mustWrite("a.txt", "needle\n")
	mustWrite("node_modules/ignored.go", "needle\n")

	res, err := Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true, IgnoreCase: true, Glob: "*.go", Context: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 1 || !res.Truncated {
		t.Fatalf("result=%+v", res)
	}
	for _, want := range []string{"a.go-1-one", "a.go:2:Needle", "a.go-3-three"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("missing %q in %q", want, res.Text)
		}
	}
	if strings.Contains(res.Text, "node_modules") || strings.Contains(res.Text, "a.txt") {
		t.Fatalf("unexpected path in %q", res.Text)
	}
}

func TestSearchRegexAndReader(t *testing.T) {
	res, err := SearchReader(context.Background(), "stdin", strings.NewReader("alpha\nbeta42\ngamma\n"), `beta\d+`, false, false, 0, 10, 500)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 1 || res.Text != "stdin:2:beta42" {
		t.Fatalf("result=%+v", res)
	}
}

func TestSearchRespectsRepositoryIgnoreFilesAndExplicitFile(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(name, body string) {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".gitignore", "ignored.txt\nnested/*.log\n")
	mustWrite("visible.txt", "needle\n")
	mustWrite("ignored.txt", "needle\n")
	mustWrite("nested/ignored.log", "needle\n")
	mustWrite("nested/visible.go", "needle\n")

	res, err := Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "visible.txt:1:needle") || !strings.Contains(res.Text, "nested/visible.go:1:needle") {
		t.Fatalf("visible files missing: %q", res.Text)
	}
	if strings.Contains(res.Text, "ignored.txt") || strings.Contains(res.Text, "ignored.log") {
		t.Fatalf("ignored files leaked: %q", res.Text)
	}

	res, err = Search(context.Background(), Options{Root: root, Path: "ignored.txt", Pattern: "needle", Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 1 || !strings.Contains(res.Text, "ignored.txt:1:needle") {
		t.Fatalf("explicit ignored file should still be searchable: %+v", res)
	}
}

func TestSearchSkipsBinaryAndHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), []byte{'n', 'e', 0, 'e'}, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Search(context.Background(), Options{Root: root, Pattern: "ne", Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.SkippedBin != 1 || res.Matches != 0 {
		t.Fatalf("binary result=%+v", res)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Search(ctx, Options{Root: root, Pattern: "ne", Literal: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search error=%v", err)
	}
}

func TestSearchHiddenFilesRequireExplicitOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".hidden.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".hidden-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden-dir", "nested.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 0 {
		t.Fatalf("hidden files should be skipped by default: %+v", res)
	}

	res, err = Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true, Hidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 2 || !strings.Contains(res.Text, ".hidden.txt:1:needle") || !strings.Contains(res.Text, ".hidden-dir/nested.txt:1:needle") {
		t.Fatalf("hidden opt-in result=%+v", res)
	}
}

func TestSearchSupportsRecursiveDoubleStarGlobs(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"internal/root.go":        "needle\n",
		"internal/nested/deep.go": "needle\n",
		"other/deep.go":           "needle\n",
	} {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true, Glob: "internal/**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 2 || !strings.Contains(res.Text, "internal/root.go:1:needle") || !strings.Contains(res.Text, "internal/nested/deep.go:1:needle") {
		t.Fatalf("recursive glob result=%+v", res)
	}
	if strings.Contains(res.Text, "other/deep.go") {
		t.Fatalf("glob escaped internal/: %q", res.Text)
	}
}

func TestSearchRejectsInvalidGlob(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Search(context.Background(), Options{Root: root, Pattern: "needle", Literal: true, Glob: "[invalid"})
	if err == nil || !strings.Contains(err.Error(), "invalid glob") {
		t.Fatalf("invalid glob error=%v", err)
	}
}
