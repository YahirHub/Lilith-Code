package tui

import (
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// SuggestionMenu renders the "/" command palette above the input bar.
// Layout mimics the pi.dev / Claude Code palette: a bullet indicator, the
// command name in the primary color, skill name in the secondary color, and a
// right-hand description column that stays aligned across rows.
type SuggestionMenu struct {
	Items    []SlashCommand
	Selected int
	Width    int
	Theme    Theme
	Query    string
}

const menuMaxVisible = 8

func (s SuggestionMenu) View() string {
	if len(s.Items) == 0 {
		return ""
	}
	start := 0
	if s.Selected >= menuMaxVisible {
		start = s.Selected - menuMaxVisible + 1
	}
	end := start + menuMaxVisible
	if end > len(s.Items) {
		end = len(s.Items)
	}

	// Align the description column to the widest visible label.
	labelWidth := 0
	for i := start; i < end; i++ {
		w := tuistyle.Width(labelFor(s.Items[i]))
		if w > labelWidth {
			labelWidth = w
		}
	}
	if labelWidth > 40 {
		labelWidth = 40
	}

	labelStyle := tuistyle.NewStyle().Foreground(s.Theme.Primary).Bold(true)
	skillLabelStyle := tuistyle.NewStyle().Foreground(s.Theme.Secondary).Bold(true)
	descStyle := tuistyle.NewStyle().Foreground(s.Theme.Muted)
	selBg := tuistyle.NewStyle().Background(s.Theme.SurfaceHover).Foreground(s.Theme.Foreground).Bold(true)
	bullet := tuistyle.NewStyle().Foreground(s.Theme.Muted).Render("·")
	bulletSel := tuistyle.NewStyle().Foreground(s.Theme.Primary).Bold(true).Render("❯")

	rows := make([]string, 0, end-start)
	innerWidth := s.Width - 4
	if innerWidth < 30 {
		innerWidth = 30
	}
	descWidth := innerWidth - labelWidth - 6
	if descWidth < 10 {
		descWidth = 10
	}

	for i := start; i < end; i++ {
		c := s.Items[i]
		label := labelFor(c)
		lstyle := labelStyle
		if isSkillItem(c) {
			lstyle = skillLabelStyle
		}
		labelCol := tuistyle.NewStyle().Width(labelWidth).Render(lstyle.Render(label))
		desc := descStyle.Render(clipMenu(descriptionFor(c), descWidth))
		bul := bullet
		if i == s.Selected {
			bul = bulletSel
		}
		line := bul + " " + labelCol + "  " + desc
		if i == s.Selected {
			line = selBg.Width(innerWidth).Render(line)
		}
		rows = append(rows, line)
	}

	box := tuistyle.NewStyle().
		Border(tuistyle.RoundedBorder()).
		BorderForeground(s.Theme.Border).
		Padding(0, 1).
		Width(s.Width).
		Render(strings.Join(rows, "\n"))
	return box
}

// labelFor returns the display label for a palette row: `/name` for regular
// commands, `skill:<name>` (secondary color) for Agent Skills.
func labelFor(c SlashCommand) string {
	if isSkillItem(c) {
		return "skill:" + c.Name
	}
	return "/" + c.Name
}

// descriptionFor strips the "skill · " prefix from skill descriptions so the
// menu column stays clean and consistent with plain commands.
func descriptionFor(c SlashCommand) string {
	if isSkillItem(c) {
		return strings.TrimPrefix(c.Description, "skill · ")
	}
	return c.Description
}

func isSkillItem(c SlashCommand) bool {
	return c.Kind == SlashItemSkill
}

func clipMenu(s string, max int) string {
	if max < 4 {
		max = 4
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
