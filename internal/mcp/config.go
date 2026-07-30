// Package mcp implements the Claude-compatible subset of Model Context
// Protocol configuration and transports used by Lilith. The wire protocol is
// JSON-RPC 2.0 and supports stdio plus modern Streamable HTTP. Legacy SSE is
// recognized explicitly so invalid configs never become silent no-ops.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type ServerConfig struct {
	Name    string            `json:"-"`
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type fileConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfigFile reads a standard .mcp.json file or a direct server map.
func LoadConfigFile(path string) map[string]ServerConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]ServerConfig{}
	}
	return ParseConfig(data)
}

// ParseConfig accepts both {"mcpServers": {...}} and the inline manifest form
// where the value itself is the server-name map.
func ParseConfig(data []byte) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	var wrapped fileConfig
	if json.Unmarshal(data, &wrapped) == nil && wrapped.MCPServers != nil {
		mergeConfigs(out, wrapped.MCPServers)
		normalizeConfigs(out)
		return out
	}
	var direct map[string]ServerConfig
	if json.Unmarshal(data, &direct) == nil {
		mergeConfigs(out, direct)
	}
	normalizeConfigs(out)
	return out
}

// Merge adds source definitions to destination, overwriting duplicate names.
func Merge(dst, src map[string]ServerConfig) {
	mergeConfigs(dst, src)
	normalizeConfigs(dst)
}

// ScopePluginConfigs namespaces plugin servers exactly as Claude exposes their
// tools: mcp__plugin_<plugin>_<server>__<tool>. It also resolves portable path
// variables and exports them to stdio subprocesses.
func ScopePluginConfigs(pluginName, pluginRoot, pluginData, projectDir string, in map[string]ServerConfig) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	for name, cfg := range in {
		scoped := "plugin_" + sanitizeConfigName(pluginName) + "_" + sanitizeConfigName(name)
		cfg.Name = scoped
		cfg.Command = expandPluginValue(cfg.Command, pluginRoot, pluginData, projectDir)
		for i := range cfg.Args {
			cfg.Args[i] = expandPluginValue(cfg.Args[i], pluginRoot, pluginData, projectDir)
		}
		cfg.URL = expandPluginValue(cfg.URL, pluginRoot, pluginData, projectDir)
		cfg.Env = cloneStrings(cfg.Env)
		for key, value := range cfg.Env {
			cfg.Env[key] = expandPluginValue(value, pluginRoot, pluginData, projectDir)
		}
		cfg.Env["CLAUDE_PLUGIN_ROOT"] = pluginRoot
		if pluginData != "" {
			cfg.Env["CLAUDE_PLUGIN_DATA"] = pluginData
		}
		if projectDir != "" {
			cfg.Env["CLAUDE_PROJECT_DIR"] = projectDir
		}
		cfg.Headers = cloneStrings(cfg.Headers)
		for key, value := range cfg.Headers {
			cfg.Headers[key] = expandPluginValue(value, pluginRoot, pluginData, projectDir)
		}
		out[scoped] = cfg
	}
	normalizeConfigs(out)
	return out
}

func expandPluginValue(value, root, data, project string) string {
	value = strings.ReplaceAll(value, "${CLAUDE_PLUGIN_ROOT}", root)
	value = strings.ReplaceAll(value, "${CLAUDE_PLUGIN_DATA}", data)
	value = strings.ReplaceAll(value, "${CLAUDE_PROJECT_DIR}", project)
	return value
}

func cloneStrings(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sanitizeConfigName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_-")
}

// LoadClaudeConfig loads user/local MCP definitions with project definitions
// taking precedence. Project config is executable configuration and therefore
// callers decide whether it is trusted before setting includeProject=true.
func LoadClaudeConfig(configDir, projectRoot string, includeProject bool) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	if home != "" {
		data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
		if err == nil {
			var raw struct {
				MCPServers map[string]ServerConfig `json:"mcpServers"`
				Projects   map[string]struct {
					MCPServers map[string]ServerConfig `json:"mcpServers"`
				} `json:"projects"`
			}
			if json.Unmarshal(data, &raw) == nil {
				mergeConfigs(out, raw.MCPServers)
				if includeProject {
					clean := filepath.Clean(projectRoot)
					for key, p := range raw.Projects {
						if filepath.Clean(key) == clean {
							mergeConfigs(out, p.MCPServers)
						}
					}
				}
			}
		}
	}
	if includeProject && strings.TrimSpace(projectRoot) != "" {
		mergeConfigs(out, LoadConfigFile(filepath.Join(projectRoot, ".mcp.json")))
	}
	normalizeConfigs(out)
	return out
}

func mergeConfigs(dst, src map[string]ServerConfig) {
	for name, cfg := range src {
		cfg.Name = name
		dst[name] = cfg
	}
}
func normalizeConfigs(all map[string]ServerConfig) {
	for name, cfg := range all {
		cfg.Name = name
		cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
		if cfg.Type == "" {
			if strings.TrimSpace(cfg.Command) != "" {
				cfg.Type = "stdio"
			} else if strings.TrimSpace(cfg.URL) != "" {
				cfg.Type = "http"
			}
		}
		all[name] = cfg
	}
}

// SelectServers resolves string references declared by an agent. When refs is
// empty, the caller can decide whether the full configured set should be used.
func SelectServers(all map[string]ServerConfig, refs []string) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	for _, ref := range refs {
		ref = strings.TrimSpace(strings.Trim(ref, `"'`))
		if cfg, ok := all[ref]; ok {
			out[ref] = cfg
		}
	}
	return out
}

// ParseInlineServers parses the documented Claude frontmatter form:
//
//   - playwright:
//     type: stdio
//     command: npx
//     args: ["-y", "@playwright/mcp@latest"]
//
// It is intentionally a focused parser, not a general YAML implementation.
func ParseInlineServers(raw string) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	min := -1
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n := len(line) - len(strings.TrimLeft(line, " \t"))
			if min < 0 || n < min {
				min = n
			}
		}
	}
	if min > 0 {
		for i, line := range lines {
			if len(line) >= min {
				lines[i] = line[min:]
			}
		}
	}
	var name string
	var cfg ServerConfig
	flush := func() {
		if name != "" {
			cfg.Name = name
			if cfg.Type == "" {
				if cfg.Command != "" {
					cfg.Type = "stdio"
				} else if cfg.URL != "" {
					cfg.Type = "http"
				}
			}
			out[name] = cfg
		}
		name = ""
		cfg = ServerConfig{}
	}
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "-") && strings.HasSuffix(strings.TrimSpace(strings.TrimPrefix(trim, "-")), ":") {
			flush()
			name = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(trim, "-")), ":")
			continue
		}
		k, v, ok := splitKV(trim)
		if !ok || name == "" {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.ToLower(k) {
		case "type":
			cfg.Type = strings.ToLower(v)
		case "command":
			cfg.Command = v
		case "url":
			cfg.URL = v
		case "args":
			cfg.Args = parseYAMLList(v)
		case "env", "headers":
			target := map[string]string{}
			baseIndent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
			for i+1 < len(lines) {
				next := lines[i+1]
				if strings.TrimSpace(next) == "" {
					i++
					continue
				}
				ind := len(next) - len(strings.TrimLeft(next, " \t"))
				if ind <= baseIndent {
					break
				}
				i++
				kk, vv, ok := splitKV(strings.TrimSpace(next))
				if ok {
					target[kk] = strings.Trim(strings.TrimSpace(vv), `"'`)
				}
			}
			if strings.EqualFold(k, "env") {
				cfg.Env = target
			} else {
				cfg.Headers = target
			}
		}
	}
	flush()
	return out
}

func splitKV(s string) (string, string, bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}
func parseYAMLList(v string) []string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		v = strings.Trim(v, "[]")
	}
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
