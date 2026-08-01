package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lilith/li/internal/tui/uikit"

	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/tools"
)

// runInit mirrors the purpose of Claude/Codex /init while keeping Lilith's
// native instructions separate: analyze the real repository, then create or
// improve a concise root LILITH.md instead of blindly generating a template.
func (m *ChatModel) runInit() uikit.Cmd {
	if m.activeTurnID != 0 {
		m.AddError("/init sólo puede ejecutarse cuando el agente está inactivo.")
		return nil
	}
	root := m.project
	if strings.TrimSpace(root) == "" {
		root, _ = os.Getwd()
	}
	target := filepath.Join(root, "LILITH.md")
	_, statErr := os.Stat(target)
	existing := statErr == nil
	state := "No existe LILITH.md; créalo en la raíz del proyecto."
	if existing {
		state = "Ya existe LILITH.md; léelo primero y mejóralo sin borrar instrucciones manuales útiles."
	}
	prompt := fmt.Sprintf(`Initialize this repository for Lilith.

%s

Before writing anything, inspect the real project using repository tools. Read the most relevant metadata and documentation (for example README, go.mod/package manifests, build files, CI config, representative source directories, and existing agent instructions). As compatibility evidence, explicitly check AGENTS.md, CLAUDE.md/CLAUDE.local.md, .claude/rules/, Cursor rules (.cursor/rules/ or .cursorrules), GitHub Copilot instructions (.github/copilot-instructions.md), Devin rules (.devin/rules/), Windsurf rules (.windsurf/rules/ or .windsurfrules), and .clinerules when present. Infer only facts supported by the repository.

Create or update exactly this native project-instructions file:
%s

The resulting LILITH.md must be concise, durable context for future coding-agent sessions, preferably under 200 lines. Include only project-specific information that is useful repeatedly: purpose, architecture/layout, build/test/lint commands, coding conventions, important workflows, generated/vendor areas to avoid, and non-obvious pitfalls. Do not copy large README sections, do not include generic advice, do not invent commands, and do not create CLAUDE.md. If Claude/AGENTS instructions exist, use them as evidence but preserve Lilith-native wording. Verify the final file against the repository before finishing.`, state, filepath.ToSlash(target))

	m.beginRewindPoint("/init")
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: "/init", Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: prompt})
	m.activeTools = m.selectToolsForPrompt(prompt, planstate.Build)
	for _, name := range []string{"read_files", "list_directory", "glob", "code_search", "run_terminal_command", "create_file", "str_replace", "apply_diff", "tool_search"} {
		if _, ok := tools.Get(name); ok {
			m.activeTools = appendUniqueTool(m.activeTools, name)
		}
	}
	m.activeTools = m.rememberToolsForMode(planstate.Build, m.activeTools)
	m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", planstate.Build))
	sort.Strings(m.activeTools)
	m.toolFallback = ""
	if err := m.beginTurnMode(planstate.Build); err != nil {
		m.AddError(err.Error())
		return nil
	}
	m.cleanupCompletedTodos()
	m.persistTurnStart()
	return uikit.Batch(m.runTurn(), m.chatMouseModeCmd())
}
