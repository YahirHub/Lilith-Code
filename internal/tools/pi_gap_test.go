package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyDiffBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "greet.txt")
	orig := "hello\nworld\nfoo\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	def, ok := Get("apply_diff")
	if !ok {
		t.Fatal("apply_diff not registered")
	}
	diff := "@@ -1,3 +1,3 @@\n hello\n-world\n+earth\n foo\n"
	env := Env{Root: dir, Seen: map[string]bool{"greet.txt": true}}
	out, err := def.Run(context.Background(), map[string]any{"path": "greet.txt", "diff": diff}, env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1 hunk") {
		t.Errorf("unexpected message: %s", out)
	}
	got, _ := os.ReadFile(path)
	want := "hello\nearth\nfoo\n"
	if string(got) != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyDiffContextMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0o644)
	def, _ := Get("apply_diff")
	diff := "@@ -1,3 +1,3 @@\n a\n-XX\n+d\n c\n"
	env := Env{Root: dir, Seen: map[string]bool{"a.txt": true}}
	if _, err := def.Run(context.Background(), map[string]any{"path": "a.txt", "diff": diff}, env); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestReadFilesRejectsImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logo.png")
	os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nblob"), 0o644)
	def, _ := Get("read_files")
	env := Env{Root: dir, Seen: map[string]bool{}}
	out, err := def.Run(context.Background(), map[string]any{"paths": []any{"logo.png"}}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[image png") {
		t.Errorf("expected image note, got %s", out)
	}
}

func TestListDirectoryLimit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	def, _ := Get("list_directory")
	env := Env{Root: dir}
	out, err := def.Run(context.Background(), map[string]any{"limit": 2}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "truncated at 2 entries of 3") {
		t.Errorf("expected truncation note, got %s", out)
	}
}
