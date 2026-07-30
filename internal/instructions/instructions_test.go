package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNativeClaudeImportsAndRules(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "LILITH.md"), []byte("native"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.md"), []byte("imported"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("claude @extra.md\n<!-- hidden -->"), 0o600); err != nil {
		t.Fatal(err)
	}
	rule := "---\npaths:\n  - src/**/*.go\n---\nGo rule"
	if err := os.WriteFile(filepath.Join(root, ".claude", "rules", "go.md"), []byte(rule), 0o600); err != nil {
		t.Fatal(err)
	}
	b := Load(Options{ConfigDir: cfg, CWD: root, NativeEnabled: true, ClaudeEnabled: true})
	p := b.StaticPrompt()
	if !strings.Contains(p, "native") || !strings.Contains(p, "imported") || strings.Contains(p, "hidden") {
		t.Fatalf("unexpected static prompt: %s", p)
	}
	if got := b.ConditionalPrompt([]string{"src/api/main.go"}); !strings.Contains(got, "Go rule") {
		t.Fatalf("path rule missing: %q", got)
	}
}

func TestClaudeMdExcludes(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	_ = os.MkdirAll(cfg, 0o700)
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("skip me"), 0o600)
	_ = os.WriteFile(filepath.Join(root, ".claude", "settings.local.json"), []byte(`{"claudeMdExcludes":["**/CLAUDE.md"]}`), 0o600)
	b := Load(Options{ConfigDir: cfg, CWD: root, ClaudeEnabled: true})
	if strings.Contains(b.StaticPrompt(), "skip me") {
		t.Fatal("excluded CLAUDE.md was loaded")
	}
}

func TestNestedClaudeMdLoadsOnlyWhenPathIsTouched(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	cfg := filepath.Join(home, ".li")
	_ = os.MkdirAll(cfg, 0o700)
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "services", "api"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "services", "web"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root instructions"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "services", "api", "CLAUDE.md"), []byte("api only"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "services", "web", "CLAUDE.md"), []byte("web only"), 0o600)

	b := Load(Options{ConfigDir: cfg, CWD: root, ClaudeEnabled: true})
	if strings.Contains(b.StaticPrompt(), "api only") || strings.Contains(b.StaticPrompt(), "web only") {
		t.Fatal("nested CLAUDE.md must be lazy")
	}
	got := b.ConditionalPrompt([]string{"services/api/main.go"})
	if !strings.Contains(got, "api only") || strings.Contains(got, "web only") {
		t.Fatalf("unexpected nested instructions: %q", got)
	}
}

func TestClaudeImportsSkipMarkdownCode(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "extra.md"), []byte("EXPANDED"), 0o600)
	body := "literal `@extra.md`\n\n```md\n@extra.md\n```\n\nreal @extra.md\n"
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(body), 0o600)
	b := Load(Options{CWD: root, ClaudeEnabled: true})
	got := b.StaticPrompt()
	if strings.Count(got, "EXPANDED") != 1 {
		t.Fatalf("expected exactly one real import expansion: %s", got)
	}
	if !strings.Contains(got, "`@extra.md`") || !strings.Contains(got, "```md\n@extra.md\n```") {
		t.Fatalf("code imports should remain literal: %s", got)
	}
}

func TestProjectExternalImportRequiresTrust(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	external := filepath.Join(base, "private.md")
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.WriteFile(external, []byte("EXTERNAL_SECRET_GUIDANCE"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("@../private.md"), 0o600)

	untrusted := Load(Options{CWD: root, ClaudeEnabled: true, ProjectTrusted: false})
	if strings.Contains(untrusted.StaticPrompt(), "EXTERNAL_SECRET_GUIDANCE") {
		t.Fatal("untrusted project must not expand imports outside the repository")
	}
	trusted := Load(Options{CWD: root, ClaudeEnabled: true, ProjectTrusted: true})
	if !strings.Contains(trusted.StaticPrompt(), "EXTERNAL_SECRET_GUIDANCE") {
		t.Fatal("trusted project should be allowed to expand external imports")
	}
}

func TestClaudeImportAllowsFiveHops(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	for i := 0; i < 5; i++ {
		from := "CLAUDE.md"
		if i > 0 {
			from = fmt.Sprintf("hop%d.md", i)
		}
		to := fmt.Sprintf("hop%d.md", i+1)
		_ = os.WriteFile(filepath.Join(root, from), []byte("@"+to), 0o600)
	}
	_ = os.WriteFile(filepath.Join(root, "hop5.md"), []byte("FIVE_HOPS_OK"), 0o600)
	b := Load(Options{CWD: root, ClaudeEnabled: true})
	if !strings.Contains(b.StaticPrompt(), "FIVE_HOPS_OK") {
		t.Fatal("Claude-compatible imports should allow five hops")
	}
}
