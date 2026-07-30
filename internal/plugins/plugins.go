// Package plugins discovers Claude Code skills-directory plugins without
// installing or copying them. Lilith intentionally starts with the portable,
// local subset: skills, commands, subagents, hooks and MCP declared by a manifest.
package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/skills"
)

type Plugin struct {
	Name        string
	Description string
	Version     string
	Root        string
	DataDir     string
	Source      string // user | project
	SkillDirs   []string
	CommandDirs []string
	AgentDirs   []string
	HookFiles   []string
	MCPFiles    []string
	HooksInline json.RawMessage
	MCPInline   json.RawMessage
}

type manifest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Skills      json.RawMessage `json:"skills"`
	Commands    json.RawMessage `json:"commands"`
	Agents      json.RawMessage `json:"agents"`
	Hooks       json.RawMessage `json:"hooks"`
	MCPServers  json.RawMessage `json:"mcpServers"`
}

// Discover scans the Claude skills-directory plugin locations. Project plugins
// are executable configuration and therefore remain invisible until the exact
// project has been trusted by Lilith.
func Discover(configDir, projectRoot string, projectTrusted bool) []Plugin {
	var out []Plugin
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
		out = append(out, scanContainer(filepath.Join(home, ".claude", "skills"), "user", home)...)
	}
	if projectTrusted && strings.TrimSpace(projectRoot) != "" {
		out = append(out, scanContainer(filepath.Join(projectRoot, ".claude", "skills"), "project", home)...)
	}
	// User first, project second. Consumers merge in this order so the project
	// version wins when both scopes define the same plugin name/component.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source == "user"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func scanContainer(container, source, home string) []Plugin {
	entries, err := os.ReadDir(container)
	if err != nil {
		return nil
	}
	var out []Plugin
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		root := filepath.Join(container, entry.Name())
		path := filepath.Join(root, ".claude-plugin", "plugin.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw manifest
		if json.Unmarshal(data, &raw) != nil {
			continue
		}
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			name = entry.Name()
		}
		if !validName(name) {
			continue
		}
		hookFiles, hooksInline := configSources(root, raw.Hooks, filepath.Join("hooks", "hooks.json"))
		mcpFiles, mcpInline := configSources(root, raw.MCPServers, ".mcp.json")
		dataDir := ""
		if home != "" {
			dataDir = filepath.Join(home, ".claude", "plugins", "data", pluginDataID(name+"-skills-dir"))
		}
		plugin := Plugin{
			Name: name, Description: strings.TrimSpace(raw.Description), Version: strings.TrimSpace(raw.Version),
			Root: root, DataDir: dataDir, Source: source,
			// Claude adds custom skill paths to the default skills/ directory,
			// while commands and agents replace their defaults when configured.
			SkillDirs:   componentPaths(root, raw.Skills, []string{"skills"}, true, false),
			CommandDirs: componentPaths(root, raw.Commands, []string{"commands"}, false, true),
			AgentDirs:   componentPaths(root, raw.Agents, []string{"agents"}, false, true),
			HookFiles:   hookFiles, MCPFiles: mcpFiles, HooksInline: hooksInline, MCPInline: mcpInline,
		}
		out = append(out, plugin)
	}
	return out
}

func componentPaths(root string, raw json.RawMessage, defaults []string, addDefaults, allowFiles bool) []string {
	values := decodePaths(raw)
	if len(values) == 0 || addDefaults {
		values = append(append([]string(nil), defaults...), values...)
	}
	if addDefaults {
		if info, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil && !info.IsDir() {
			values = append(values, ".")
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "${CLAUDE_PLUGIN_ROOT}", root))
		if value == "" {
			continue
		}
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "./")))
		}
		path = filepath.Clean(path)
		if !inside(root, path) || seen[path] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || (!info.IsDir() && !allowFiles) {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func configSources(root string, raw json.RawMessage, defaultPath string) ([]string, json.RawMessage) {
	trimmed := bytesTrim(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		path := filepath.Join(root, filepath.FromSlash(defaultPath))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return []string{path}, nil
		}
		return nil, nil
	}
	if trimmed[0] == '{' {
		return nil, append(json.RawMessage(nil), trimmed...)
	}
	var out []string
	for _, value := range decodePaths(trimmed) {
		value = strings.TrimSpace(strings.ReplaceAll(value, "${CLAUDE_PLUGIN_ROOT}", root))
		if value == "" {
			continue
		}
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "./")))
		}
		path = filepath.Clean(path)
		if !inside(root, path) {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			out = append(out, path)
		}
	}
	return uniquePaths(out), nil
}

func bytesTrim(raw json.RawMessage) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func uniquePaths(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, path := range in {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func pluginDataID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func decodePaths(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			if i == 0 && r == '-' {
				return false
			}
			continue
		}
		return false
	}
	return !strings.HasSuffix(name, "-")
}

// LoadAgents imports one plugin's subagent definitions under Claude's
// plugin-name:agent-name namespace. Plugin agents cannot smuggle hook/MCP or
// permission-mode configuration through their frontmatter; those components
// are owned by the plugin runtime and require separate trust handling.
func LoadAgents(plugin Plugin) []agents.Agent {
	var opts agents.LoadOptions
	if plugin.Source == "project" {
		opts.ProjectDirs = plugin.AgentDirs
	} else {
		opts.UserDirs = plugin.AgentDirs
	}
	list := agents.Load(opts)
	for i := range list {
		list[i].Name = scoped(plugin.Name, list[i].Name)
		list[i].Source = plugin.Source
		list[i].PluginRoot = plugin.Root
		list[i].HooksRaw = ""
		list[i].MCPRaw = ""
		list[i].MCPServers = nil
		list[i].PermissionMode = ""
		for j, skillName := range list[i].Skills {
			if !strings.Contains(skillName, ":") {
				list[i].Skills[j] = scoped(plugin.Name, skillName)
			}
		}
	}
	return list
}

// LoadSkills imports modern skills and legacy commands under the plugin
// namespace. A modern SKILL.md wins over a legacy command with the same name.
func LoadSkills(plugin Plugin) []skills.Skill {
	seen := map[string]skills.Skill{}
	for _, skill := range skills.LoadLegacyCommandDirs(plugin.CommandDirs, plugin.Source) {
		skill.Name = scoped(plugin.Name, skill.Name)
		skill.Source = plugin.Source
		skill.PluginRoot = plugin.Root
		if skill.Agent != "" && !strings.Contains(skill.Agent, ":") {
			skill.Agent = scoped(plugin.Name, skill.Agent)
		}
		seen[strings.ToLower(skill.Name)] = skill
	}
	var opts skills.LoadOptions
	if plugin.Source == "project" {
		opts.ProjectDirs = plugin.SkillDirs
	} else {
		opts.UserDirs = plugin.SkillDirs
	}
	for _, skill := range skills.Load(opts) {
		skill.Name = scoped(plugin.Name, skill.Name)
		skill.Source = plugin.Source
		skill.PluginRoot = plugin.Root
		if skill.Agent != "" && !strings.Contains(skill.Agent, ":") {
			skill.Agent = scoped(plugin.Name, skill.Agent)
		}
		seen[strings.ToLower(skill.Name)] = skill
	}
	out := make([]skills.Skill, 0, len(seen))
	for _, skill := range seen {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func scoped(pluginName, componentName string) string {
	return strings.TrimSpace(pluginName) + ":" + strings.TrimSpace(componentName)
}
