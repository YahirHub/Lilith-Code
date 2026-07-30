package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentPathsMatchClaudeScopes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	cfg := filepath.Join(t.TempDir(), ".li")
	got := AgentDir(cfg, root, "reviewer", Project)
	if got != filepath.Join(root, ".claude", "agent-memory", "reviewer") {
		t.Fatalf("project path=%s", got)
	}
	got = AgentDir(cfg, root, "reviewer", Local)
	if got != filepath.Join(root, ".claude", "agent-memory-local", "reviewer") {
		t.Fatalf("local path=%s", got)
	}
}

func TestReadStartupIsBounded(t *testing.T) {
	d := t.TempDir()
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("line\n")
	}
	if err := os.WriteFile(filepath.Join(d, "MEMORY.md"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadStartup(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(got, "\n")) != MaxStartupLines {
		t.Fatalf("lines=%d", len(strings.Split(got, "\n")))
	}
}

func TestMemoryWriteCannotEscape(t *testing.T) {
	d := t.TempDir()
	if err := WriteFile(d, "../escape", "x"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestClaudeProjectDirHonorsTrustedScopedOverride(t *testing.T) {
	t.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", "")
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"autoMemoryDirectory":"~/user-memory"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"autoMemoryDirectory":"`+filepath.ToSlash(filepath.Join(root, "project-memory"))+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ClaudeProjectDir(cfg, root, false); got != filepath.Join(home, "user-memory") {
		t.Fatalf("untrusted project override applied: %s", got)
	}
	if got := ClaudeProjectDir(cfg, root, true); got != filepath.Join(root, "project-memory") {
		t.Fatalf("trusted project override missing: %s", got)
	}
}
