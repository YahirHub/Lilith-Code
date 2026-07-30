package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillsDirectoryPluginsAndTrustGate(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".li")
	project := t.TempDir()
	writePlugin := func(container, dir, manifest string) {
		root := filepath.Join(container, dir)
		if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "skills", "review"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writePlugin(filepath.Join(home, ".claude", "skills"), "personal-dir", `{"name":"personal","version":"1.0.0"}`)
	writePlugin(filepath.Join(project, ".claude", "skills"), "project-dir", `{"name":"project-plugin"}`)

	got := Discover(configDir, project, false)
	if len(got) != 1 || got[0].Name != "personal" || got[0].Source != "user" {
		t.Fatalf("untrusted discovery=%#v", got)
	}
	got = Discover(configDir, project, true)
	if len(got) != 2 || got[1].Name != "project-plugin" || got[1].Source != "project" {
		t.Fatalf("trusted discovery=%#v", got)
	}
	if len(got[0].SkillDirs) != 1 || len(got[0].AgentDirs) != 1 {
		t.Fatalf("components=%#v", got[0])
	}
}

func TestManifestPathsCannotEscapePluginRoot(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".li")
	root := filepath.Join(home, ".claude", "skills", "safe")
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"name":"safe","skills":"../outside"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Discover(configDir, "", false)
	if len(got) != 1 || len(got[0].SkillDirs) != 0 {
		t.Fatalf("escaped component accepted: %#v", got)
	}
}

func TestLoadComponentsUsesPluginNamespaceAndAgentSecurityBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nagent: worker\n---\nReview it."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentDoc := "---\nname: worker\ndescription: Worker\nskills: [review]\npermissionMode: bypassPermissions\nmcpServers: [unsafe]\nhooks:\n  PreToolUse: []\n---\nWork."
	if err := os.WriteFile(filepath.Join(root, "agents", "worker.md"), []byte(agentDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	plugin := Plugin{Name: "quality", Root: root, Source: "user", SkillDirs: []string{filepath.Join(root, "skills")}, AgentDirs: []string{filepath.Join(root, "agents")}}
	loadedSkills := LoadSkills(plugin)
	if len(loadedSkills) != 1 || loadedSkills[0].Name != "quality:review" || loadedSkills[0].Agent != "quality:worker" || loadedSkills[0].PluginRoot != root {
		t.Fatalf("skills=%#v", loadedSkills)
	}
	loadedAgents := LoadAgents(plugin)
	if len(loadedAgents) != 1 || loadedAgents[0].Name != "quality:worker" || loadedAgents[0].PluginRoot != root {
		t.Fatalf("agents=%#v", loadedAgents)
	}
	if len(loadedAgents[0].Skills) != 1 || loadedAgents[0].Skills[0] != "quality:review" {
		t.Fatalf("agent skills=%#v", loadedAgents[0].Skills)
	}
	if loadedAgents[0].PermissionMode != "" || loadedAgents[0].HooksRaw != "" || loadedAgents[0].MCPRaw != "" || len(loadedAgents[0].MCPServers) != 0 {
		t.Fatalf("plugin agent security fields leaked: %#v", loadedAgents[0])
	}
}

func TestDiscoverPluginRuntimeComponentsAndCustomPaths(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".li")
	root := filepath.Join(home, ".claude", "skills", "runtime")
	for _, dir := range []string{
		filepath.Join(root, ".claude-plugin"),
		filepath.Join(root, "skills", "default"),
		filepath.Join(root, "extra-skills", "extra"),
		filepath.Join(root, "config"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{
		filepath.Join(root, "skills", "default", "SKILL.md"),
		filepath.Join(root, "extra-skills", "extra", "SKILL.md"),
		filepath.Join(root, "deploy.md"),
		filepath.Join(root, "reviewer.md"),
		filepath.Join(root, "config", "hooks.json"),
	} {
		if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{
		"name":"runtime-tools",
		"skills":"./extra-skills",
		"commands":"./deploy.md",
		"agents":"./reviewer.md",
		"hooks":"./config/hooks.json",
		"mcpServers":{"db":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/server"}}
	}`
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Discover(configDir, "", false)
	if len(got) != 1 {
		t.Fatalf("plugins=%#v", got)
	}
	plugin := got[0]
	if len(plugin.SkillDirs) != 2 {
		t.Fatalf("custom skills should extend default: %#v", plugin.SkillDirs)
	}
	if len(plugin.CommandDirs) != 1 || plugin.CommandDirs[0] != filepath.Join(root, "deploy.md") {
		t.Fatalf("command paths=%#v", plugin.CommandDirs)
	}
	if len(plugin.AgentDirs) != 1 || plugin.AgentDirs[0] != filepath.Join(root, "reviewer.md") {
		t.Fatalf("agent paths=%#v", plugin.AgentDirs)
	}
	if len(plugin.HookFiles) != 1 || plugin.HookFiles[0] != filepath.Join(root, "config", "hooks.json") {
		t.Fatalf("hook files=%#v", plugin.HookFiles)
	}
	if len(plugin.MCPInline) == 0 || len(plugin.MCPFiles) != 0 {
		t.Fatalf("inline MCP not retained: %#v", plugin)
	}
	wantData := filepath.Join(home, ".claude", "plugins", "data", "runtime-tools-skills-dir")
	if plugin.DataDir != wantData {
		t.Fatalf("data dir=%q want %q", plugin.DataDir, wantData)
	}
}
