package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

// ConfigScreen es la pantalla dedicada de /config. Sustituye al antiguo
// "chat.AddSystem(Config: ...)" con un espacio real donde el usuario puede
// activar/desactivar features de Lilith (por ahora: skills al estilo Claude
// Code). Está pensada para crecer con más toggles sin ensuciar el chat.
type ConfigScreen struct {
	ctx      *AppContext
	settings config.Settings
	cursor   int
	message  string
	danger   string
	loaded   []skills.Skill
}

// NewConfigScreen construye la pantalla leyendo la configuración persistida.
func NewConfigScreen(ctx *AppContext) *ConfigScreen {
	s, _ := config.Load(ctx.ConfigDir)
	loaded := skills.Load(skills.LoadOptions{
		UserDir:    skills.UserDir(ctx.ConfigDir),
		ProjectDir: skills.ProjectDir(currentProject()),
	})
	return &ConfigScreen{ctx: ctx, settings: s, loaded: loaded}
}

// configRow describe una fila renderizable / navegable.
type configRow struct {
	label string
	value string
	hint  string
	kind  string // "toggle" | "info" | "action"
}

func (c *ConfigScreen) rows() []configRow {
	toggle := "off"
	if c.settings.SkillsEnabled {
		toggle = "on"
	}
	return []configRow{
		{
			label: "Claude Code skills",
			value: toggle,
			hint:  fmt.Sprintf("%d skill(s) detectada(s) en ~/.li/skills y ./.li/skills", len(c.loaded)),
			kind:  "toggle",
		},
		{
			label: "Ruta de configuración",
			value: c.ctx.ConfigDir,
			kind:  "info",
		},
		{
			label: "Skills disponibles",
			value: c.summarizeSkills(),
			kind:  "info",
		},
		{
			label: "Volver al chat",
			kind:  "action",
		},
	}
}

func (c *ConfigScreen) summarizeSkills() string {
	if len(c.loaded) == 0 {
		return "(ninguna — crea ~/.li/skills/<nombre>/SKILL.md con frontmatter name/description)"
	}
	names := make([]string, 0, len(c.loaded))
	for _, s := range c.loaded {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

func (c *ConfigScreen) Init() tea.Cmd { return nil }

func (c *ConfigScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		key := v.String()
		rows := c.rows()
		switch key {
		case "esc", "q":
			return c, switchToChat()
		case "ctrl+c":
			return c, tea.Quit
		case "up", "k":
			if c.cursor > 0 {
				c.cursor--
			}
			return c, nil
		case "down", "j":
			if c.cursor < len(rows)-1 {
				c.cursor++
			}
			return c, nil
		case "enter", " ":
			row := rows[c.cursor]
			switch row.kind {
			case "toggle":
				c.settings.SkillsEnabled = !c.settings.SkillsEnabled
				if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
					c.danger = "No se pudo guardar: " + err.Error()
					return c, nil
				}
				c.danger = ""
				state := "desactivadas"
				if c.settings.SkillsEnabled {
					state = "activadas"
					c.loaded = skills.Load(skills.LoadOptions{
						UserDir:    skills.UserDir(c.ctx.ConfigDir),
						ProjectDir: skills.ProjectDir(currentProject()),
					})
				}
				c.message = fmt.Sprintf("Skills %s. %d disponibles.", state, len(c.loaded))
			case "action":
				return c, switchToChat()
			}
			return c, nil
		}
	}
	return c, nil
}

func (c *ConfigScreen) View() string {
	s := c.ctx.Styles
	w := c.ctx.Width - 4
	if w < 30 {
		w = 30
	}
	if w > 100 {
		w = 100
	}
	header := s.Accent.Render("Configuración de Lilith")
	sub := s.Muted.Render("↑↓ navegar   Enter / Espacio alternar   Esc volver")
	rows := c.rows()
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(sub)
	b.WriteString("\n\n")
	for i, r := range rows {
		selected := i == c.cursor
		label := r.label
		val := r.value
		line := "  " + lipgloss.NewStyle().Foreground(s.Theme.Foreground).Render(label)
		if val != "" {
			line += "   " + lipgloss.NewStyle().Foreground(s.Theme.Primary).Render(val)
		}
		if selected {
			line = lipgloss.NewStyle().
				Background(s.Theme.SurfaceHover).
				Foreground(s.Theme.Foreground).
				Width(w).
				Render("❯ " + label + "   " + val)
		}
		b.WriteString(line)
		b.WriteString("\n")
		if r.hint != "" {
			b.WriteString(s.Muted.Render("    " + r.hint))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if c.message != "" {
		b.WriteString(s.Muted.Render("· " + c.message))
		b.WriteString("\n")
	}
	if c.danger != "" {
		b.WriteString(s.Danger.Render("✗ " + c.danger))
		b.WriteString("\n")
	}
	return b.String()
}

var _ tea.Model = (*ConfigScreen)(nil)

// currentProject wraps os.Getwd() para poder ser stubbeado en tests.
func currentProject() string {
	dir, err := getCwd()
	if err != nil {
		return "."
	}
	return dir
}
