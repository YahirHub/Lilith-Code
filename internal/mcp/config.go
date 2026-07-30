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
		data, err := os.ReadFile(filepath.Join(projectRoot, ".mcp.json"))
		if err == nil {
			var raw fileConfig
			if json.Unmarshal(data, &raw) == nil {
				mergeConfigs(out, raw.MCPServers)
			}
		}
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
