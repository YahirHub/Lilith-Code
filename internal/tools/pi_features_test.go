package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFilesOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("l1\nl2\nl3\nl4\nl5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: dir}
	d, _ := Get("read_files")
	out, err := d.Run(context.Background(), map[string]any{
		"paths":  []any{"a.txt"},
		"offset": 2.0,
		"limit":  2.0,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "l2\nl3") {
		t.Fatalf("expected l2/l3 window, got:\n%s", out)
	}
	if !strings.Contains(out, "use offset=4 to continue") {
		t.Fatalf("expected next-offset hint, got:\n%s", out)
	}
}

func TestStrReplaceMultiEditsAndFuzzy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "b.txt")
	// The file has a smart quote; the model will hand us an ASCII quote.
	if err := os.WriteFile(p, []byte("alpha \u201Chello\u201D beta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Root: dir}
	d, _ := Get("str_replace")
	_, err := d.Run(context.Background(), map[string]any{
		"path": "b.txt",
		"edits": []any{
			map[string]any{"old": `alpha "hello" beta`, "new": `alpha WORLD beta`}, // fuzzy quote match
			map[string]any{"old": "gamma", "new": "GAMMA"},
		},
	}, env)
	if err != nil {
		t.Fatalf("multi-edit failed: %v", err)
	}
	got, _ := os.ReadFile(p)
	want := "alpha WORLD beta\nGAMMA\n"
	if string(got) != want {
		t.Fatalf("want %q, got %q", want, string(got))
	}
}

func TestStrReplaceRejectsEmptyOld(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.txt")
	os.WriteFile(p, []byte("x\n"), 0o644)
	env := Env{Root: dir}
	d, _ := Get("str_replace")
	_, err := d.Run(context.Background(), map[string]any{"path": "c.txt", "old": "", "new": "y"}, env)
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty-old error, got %v", err)
	}
}

func TestStrReplaceAcceptsStringifiedEditsAndPreservesBOMCRLF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "windows.txt")
	before := "\uFEFFalpha\r\nbeta\r\n"
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Execute(context.Background(), "str_replace", map[string]any{
		"path":  "windows.txt",
		"edits": `[{"old":"alpha\nbeta","new":"ALPHA\nBETA"}]`,
	}, Env{Root: dir})
	if err != nil {
		t.Fatalf("stringified edits should be normalized: %v", err)
	}
	if !strings.Contains(out, "1 replacement") {
		t.Fatalf("unexpected result: %q", out)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "\uFEFFALPHA\r\nBETA\r\n"
	if string(got) != want {
		t.Fatalf("BOM/CRLF not preserved: want %q got %q", want, got)
	}
}

func TestStrReplaceFuzzyUsesNFKC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "unicode.txt")
	if err := os.WriteFile(p, []byte("ＡＢＣ\u00A0value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "str_replace", map[string]any{
		"path": "unicode.txt",
		"old":  "ABC value",
		"new":  "normalized",
	}, Env{Root: dir})
	if err != nil {
		t.Fatalf("NFKC fuzzy match failed: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "normalized\n" {
		t.Fatalf("unexpected fuzzy result: %q", got)
	}
}

func TestStrReplaceMismatchReportsCurrentHashAndNearbyLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "report.md")
	content := "# Report\n\n## Current section\nactual body\n\n## End\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "str_replace", map[string]any{
		"path": "report.md",
		"old":  "## Current section\nstale body",
		"new":  "## Current section\nnew body",
	}, Env{Root: dir})
	if err == nil {
		t.Fatal("expected stale-target error")
	}
	message := err.Error()
	for _, want := range []string{"target text was not found", "current_sha256:", "nearby_current_text:", "3 | ## Current section", "retry_hint: read_files"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing %q in diagnostic:\n%s", want, message)
		}
	}
	got, readErr := os.ReadFile(p)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != content {
		t.Fatalf("mismatch must not modify file: %q", got)
	}
}
