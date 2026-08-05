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
