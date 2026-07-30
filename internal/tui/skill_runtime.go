package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/hooks"
	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/skills"
	"github.com/lilith/li/internal/tools"
)

// applySkillTurnOverrides applies Claude-compatible skill metadata only to the
// current turn. The next ordinary user prompt starts from the session model and
// normal tool pool again.
func (m *ChatModel) applySkillTurnOverrides(sk skills.Skill) error {
	if strings.TrimSpace(sk.Model) != "" && !strings.EqualFold(strings.TrimSpace(sk.Model), "inherit") {
		p, model, err := resolvePortableModel(m.ctx.Providers, m.turnProvider, m.turnModel, sk.Model)
		if err != nil {
			return err
		}
		m.turnProvider = p.ID
		m.turnModel = model
	}
	m.turnReasoningEffort = normalizeEffort(sk.Effort)
	m.turnDeniedTools = portableDeniedTools(sk.DisallowedTools)

	settings, _ := config.Load(m.ctx.ConfigDir)
	trustedProjectSkill := sk.Source != "project" || config.IsProjectTrusted(settings, m.project)
	if settings.HooksEnabled && trustedProjectSkill && strings.TrimSpace(sk.HooksRaw) != "" {
		m.turnSkillHooks = hooks.ParseFrontmatter(sk.HooksRaw, sk.BaseDir)
	}
	return nil
}

func (m *ChatModel) skillShellExecutionAllowed(sk skills.Skill) bool {
	settings, _ := config.Load(m.ctx.ConfigDir)
	if sk.Source == "project" && !config.IsProjectTrusted(settings, m.project) {
		return false
	}
	return !skills.ShellExecutionDisabled(m.ctx.ConfigDir)
}

func (m *ChatModel) expandSkillShell(ctx context.Context, sk skills.Skill, body string) (string, error) {
	return skills.ExpandShellCommands(ctx, sk, body, m.project, m.skillShellExecutionAllowed(sk))
}

func (m *ChatModel) skillAllowedToolsCanGrant(sk skills.Skill) bool {
	if sk.Source != "project" {
		return true
	}
	settings, _ := config.Load(m.ctx.ConfigDir)
	return config.IsProjectTrusted(settings, m.project)
}

func (m *ChatModel) materializeSkillAllowedTools(sk skills.Skill) {
	if !m.skillAllowedToolsCanGrant(sk) {
		return
	}
	for _, external := range sk.AllowedTools {
		for _, name := range portableToolNames(external) {
			if _, ok := tools.Get(name); ok {
				m.activeTools = appendUniqueTool(m.activeTools, name)
			}
		}
	}
}

func (m *ChatModel) toolDeniedForTurn(name string) bool {
	if len(m.turnDeniedTools) == 0 {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return m.turnDeniedTools[name]
}

func portableDeniedTools(values []string) map[string]bool {
	out := map[string]bool{}
	for _, external := range values {
		for _, name := range portableToolNames(external) {
			out[strings.ToLower(name)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func portableToolNames(external string) []string {
	v := strings.TrimSpace(external)
	if i := strings.IndexByte(v, '('); i > 0 {
		v = v[:i]
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "read":
		return []string{"read_files"}
	case "glob":
		return []string{"glob"}
	case "grep", "search":
		return []string{"code_search"}
	case "list", "list_directory":
		return []string{"list_directory"}
	case "bash", "powershell":
		return []string{"run_terminal_command"}
	case "edit":
		return []string{"str_replace", "apply_diff"}
	case "write":
		return []string{"create_file", "str_replace", "apply_diff"}
	case "webfetch":
		return []string{"read_url"}
	case "websearch":
		return []string{"web_search"}
	case "todowrite":
		return []string{"todo_write"}
	case "skill":
		return []string{"list_skills", "skill_read", "skill_search", "skill_files"}
	case "toolsearch":
		return []string{"tool_search"}
	case "agent", "task":
		return []string{"Agent"}
	default:
		if _, ok := tools.Get(v); ok {
			return []string{v}
		}
		if _, ok := tools.Get(strings.ToLower(v)); ok {
			return []string{strings.ToLower(v)}
		}
		if strings.HasPrefix(strings.ToLower(v), "mcp__") {
			return []string{v}
		}
		return nil
	}
}

func normalizeEffort(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "medium", "high", "xhigh", "max":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func resolvePortableModel(cfg providers.Config, parentProvider, parentModel, requested string) (providers.Provider, string, error) {
	want := strings.TrimSpace(requested)
	if want == "" || strings.EqualFold(want, "inherit") {
		if p := cfg.FindProvider(parentProvider); p != nil {
			return *p, parentModel, nil
		}
		return providers.Provider{}, "", errors.New("proveedor activo no encontrado")
	}
	if strings.Contains(want, "/") {
		parts := strings.SplitN(want, "/", 2)
		if p := cfg.FindProvider(parts[0]); p != nil && portableProviderHasModel(*p, parts[1]) {
			return *p, parts[1], nil
		}
	}
	needle := strings.ToLower(want)
	if p := cfg.FindProvider(parentProvider); p != nil {
		if id := portableFindModel(*p, needle); id != "" {
			return *p, id, nil
		}
	}
	for _, p := range cfg.Providers {
		if id := portableFindModel(p, needle); id != "" {
			return p, id, nil
		}
	}
	if needle == "sonnet" || needle == "opus" || needle == "haiku" {
		if p := cfg.FindProvider(parentProvider); p != nil {
			return *p, parentModel, nil
		}
	}
	return providers.Provider{}, "", fmt.Errorf("modelo de skill %q no está configurado; usa inherit o provider/model", requested)
}

func portableFindModel(p providers.Provider, want string) string {
	for _, model := range p.Models {
		if strings.EqualFold(model.ID, want) || strings.EqualFold(model.Name, want) {
			return model.ID
		}
	}
	if want == "sonnet" || want == "opus" || want == "haiku" {
		for _, model := range p.Models {
			if strings.Contains(strings.ToLower(model.ID+" "+model.Name), want) {
				return model.ID
			}
		}
	}
	return ""
}

func portableProviderHasModel(p providers.Provider, id string) bool {
	for _, model := range p.Models {
		if model.ID == id {
			return true
		}
	}
	return false
}
