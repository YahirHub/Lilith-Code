package tui

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

func TestFilterCommandsRanksExactLoginFirst(t *testing.T) {
	rows := FilterCommands("/login")
	if len(rows) == 0 || rows[0].Name != "login" {
		t.Fatalf("primer resultado=%#v", rows)
	}
}

func TestExactSkillTokenUsesSecondaryColorInInput(t *testing.T) {
	m := newInputTestChat(t)
	m.skillNameCache["web-design"] = struct{}{}
	m.skillNameCacheLoaded = true
	m.textarea.SetValue("/web-design crea una web")
	view := m.inputBoxView(100)
	want := tuistyle.NewStyle().Foreground(m.ctx.Styles.Theme.Secondary).Bold(true).Render("/web-design")
	if !strings.Contains(view, want) {
		t.Fatalf("la skill no usa el color secundario en el input:\n%s", view)
	}
}

func TestPaletteTabCompletesWithTrailingSpace(t *testing.T) {
	m := newInputTestChat(t)
	m.paletteOpen = true
	m.paletteRows = []SlashCommand{{Name: "web-design", Kind: SlashItemSkill}}
	m.paletteIdx = 0
	_, _ = m.Update(uikit.KeyMsg{Type: uikit.KeyTab})
	if got := m.textarea.Value(); got != "/web-design " {
		t.Fatalf("autocompletado=%q", got)
	}
	if m.paletteOpen {
		t.Fatal("la paleta debe cerrarse al completar el token con espacio")
	}
}

func TestSkillRowsAreRecognizedAndStyledSeparately(t *testing.T) {
	row := SlashCommand{Name: "web-design", Kind: SlashItemSkill, Description: "skill · Diseño web"}
	if !isSkillItem(row) {
		t.Fatal("la fila no fue reconocida como skill")
	}
	if got := labelFor(row); got != "skill:web-design" {
		t.Fatalf("label=%q", got)
	}
	menu := SuggestionMenu{Items: []SlashCommand{row}, Width: 80, Theme: DefaultTheme()}.View()
	if !strings.Contains(stripANSI(menu), "skill:web-design") {
		t.Fatalf("la skill no aparece en la paleta:\n%s", stripANSI(menu))
	}
}

func TestSlashCommandsAreOwnedByFeatureModules(t *testing.T) {
	owners := map[string]string{
		"help": "core.help", "init": "core.project", "goal": "core.goal", "resume": "core.goal",
		"plan": "core.mode", "build": "core.mode", "compact": "core.compaction",
		"rewind": "core.rewind", "fork": "core.fork", "memory": "core.memory",
		"mcp": "core.mcp", "tasks": "core.agents", "subtask": "core.agents",
		"plugins": "core.plugins", "reload-plugins": "core.plugins", "agents": "core.agents",
		"login": "core.providers", "providers": "core.providers", "models": "core.providers",
		"config": "core.config", "clear": "core.session", "history": "core.session",
		"bash": "core.shell", "exit": "core.session", "modules": "core.modules",
	}
	rows := Commands()
	if len(rows) != len(owners) {
		t.Fatalf("comandos registrados=%d, esperados=%d: %#v", len(rows), len(owners), rows)
	}
	for _, row := range rows {
		want := owners[row.Name]
		if want == "" {
			t.Fatalf("/%s apareció sin módulo esperado: %+v", row.Name, row)
		}
		if row.ModuleID != want {
			t.Fatalf("/%s pertenece a %q; esperado %q", row.Name, row.ModuleID, want)
		}
		if row.ModuleID == "core.commands" {
			t.Fatalf("/%s sigue dependiendo del mega-módulo de compatibilidad", row.Name)
		}
	}
}

func TestSkillSlashPrefixIsOwnedBySkillModule(t *testing.T) {
	route, moduleID, target, ok := FindModuleRoute("/skill:frontend-development")
	if !ok || moduleID != "core.skills" || target != "frontend-development" {
		t.Fatalf("route=%+v module=%q target=%q ok=%v", route, moduleID, target, ok)
	}
	if route.Kind != "skill" || route.Handler == nil {
		t.Fatalf("route skill inválido: %+v", route)
	}
}

func TestModulesCommandShowsRegistry(t *testing.T) {
	m := newInputTestChat(t)
	cmd := FindCommand("modules")
	if cmd == nil {
		t.Fatal("/modules no está registrado")
	}
	_ = cmd.Run(m.ctx, m, "")
	if len(m.messages) == 0 {
		t.Fatal("/modules no produjo salida")
	}
	last := m.messages[len(m.messages)-1].Content
	for _, want := range []string{"core.help", "core.mode", "core.modules", "core.rewind", "core.skills", "core.session"} {
		if !strings.Contains(last, want) {
			t.Fatalf("/modules no contiene %s:\n%s", want, last)
		}
	}
}

func TestSlashSearchQueryUsesDynamicModuleRouteTarget(t *testing.T) {
	if got := slashSearchQuery("/skill:Frontend-Development"); got != "frontend-development" {
		t.Fatalf("query dinámica=%q", got)
	}
	if got := slashSearchQuery("/login"); got != "login" {
		t.Fatalf("query exacta=%q", got)
	}
}
