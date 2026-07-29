package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

type configSection string

const (
	configSectionGeneral  configSection = "general"
	configSectionSearch   configSection = "search"
	configSectionSecurity configSection = "security"
)

var configSections = []configSection{
	configSectionGeneral,
	configSectionSearch,
	configSectionSecurity,
}

// ConfigScreen is the interactive `/config` screen. The card design is now the
// stable settings treatment: sections share the same top navigation and the
// focused interactive card receives the complete moving border.
type ConfigScreen struct {
	ctx      *AppContext
	settings config.Settings
	section  configSection
	focus    string
	message  string
	danger   string
	loaded   []skills.Skill
}

func NewConfigScreen(ctx *AppContext) *ConfigScreen {
	s, _ := config.Load(ctx.ConfigDir)
	loaded := skills.Load(skills.DefaultLoadOptions(ctx.ConfigDir, currentProject()))
	return &ConfigScreen{
		ctx:      ctx,
		settings: s,
		loaded:   loaded,
		section:  configSectionGeneral,
		focus:    "skills",
	}
}

func (c *ConfigScreen) Init() tea.Cmd { return nil }

func (c *ConfigScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.MouseMsg:
		e, ok := mouseLeftPress(v)
		if !ok {
			return c, nil
		}
		_, hits := c.layout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return c, nil
		}
		if strings.HasPrefix(hit.id, "section:") {
			c.setSection(configSection(strings.TrimPrefix(hit.id, "section:")))
			return c, nil
		}
		c.focus = hit.id
		switch hit.id {
		case "skills":
			return c.toggleSkills()
		case "back":
			return c, switchToChat()
		}

	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			return c, tea.Quit
		case "esc", "q":
			return c, switchToChat()
		case "tab", "right", "l":
			c.rotateSection(1)
			return c, nil
		case "shift+tab", "left", "h":
			c.rotateSection(-1)
			return c, nil
		case "down", "j":
			c.moveFocus(1)
			return c, nil
		case "up", "k":
			c.moveFocus(-1)
			return c, nil
		case "enter", " ":
			switch c.focus {
			case "skills":
				if c.section == configSectionGeneral {
					return c.toggleSkills()
				}
			case "back":
				return c, switchToChat()
			}
		}
	}
	return c, nil
}

func (c *ConfigScreen) setSection(section configSection) {
	for _, candidate := range configSections {
		if candidate != section {
			continue
		}
		c.section = section
		if section == configSectionGeneral {
			c.focus = "skills"
		} else {
			c.focus = "back"
		}
		return
	}
}

func (c *ConfigScreen) rotateSection(delta int) {
	idx := 0
	for i, section := range configSections {
		if section == c.section {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(configSections)
	if idx < 0 {
		idx += len(configSections)
	}
	c.setSection(configSections[idx])
}

func (c *ConfigScreen) moveFocus(delta int) {
	order := []string{"back"}
	if c.section == configSectionGeneral {
		order = []string{"skills", "back"}
	}
	idx := 0
	for i, id := range order {
		if id == c.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(order)
	if idx < 0 {
		idx += len(order)
	}
	c.focus = order[idx]
}

func (c *ConfigScreen) toggleSkills() (tea.Model, tea.Cmd) {
	c.settings.SkillsEnabled = !c.settings.SkillsEnabled
	if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
		c.danger = "No se pudo guardar: " + err.Error()
		return c, nil
	}
	c.danger = ""
	state := "desactivadas"
	if c.settings.SkillsEnabled {
		state = "activadas"
		c.loaded = skills.Load(skills.DefaultLoadOptions(c.ctx.ConfigDir, currentProject()))
	}
	c.message = fmt.Sprintf("Skills %s. %d disponibles.", state, len(c.loaded))
	return c, nil
}

func (c *ConfigScreen) layout() (string, []settingsHit) {
	s := c.ctx.Styles
	w := settingsContentWidth(c.ctx.Width)
	canvas := newSettingsCanvas(w)
	canvas.block(settingsHeader(s, "Configuración", c.sectionSubtitle()))
	canvas.blank()
	canvas.block(c.sectionPicker(w))
	canvas.blank()

	if c.section == configSectionGeneral {
		canvas.block(c.skillsFocusCard(w))
		canvas.blank()
		canvas.block(settingsCard(s, settingsCardSpec{
			Title:       "Ruta de configuración",
			Description: c.ctx.ConfigDir,
			Badge:       "INFO",
			Width:       w,
		}))
	} else {
		canvas.block(settingsCard(s, settingsCardSpec{
			Title:       "Características en desarrollo",
			Description: c.developmentDescription(),
			Badge:       "PRÓXIMO",
			Width:       w,
		}))
	}

	canvas.blank()
	canvas.block(c.backFocusCard(w))
	if c.message != "" && c.section == configSectionGeneral {
		canvas.blank()
		canvas.line(s.Success.Render("· " + settingsWrapPlain(c.message, w)))
	}
	if c.danger != "" {
		canvas.blank()
		canvas.line(s.Danger.Render("Error: " + settingsWrapPlain(c.danger, w)))
	}
	canvas.blank()
	canvas.block(settingsFooter(s, "Tab/Shift+Tab o ←→ cambiar sección · ↑↓ mover foco · Enter/Espacio usar · clic · Esc volver"))
	return canvas.render(c.ctx.Width)
}

func (c *ConfigScreen) sectionPicker(width int) settingsBlock {
	return settingsButtonGroup(c.ctx.Styles, width,
		settingsButtonSpec{ID: "section:general", Label: "General", Focused: c.section == configSectionGeneral},
		settingsButtonSpec{ID: "section:search", Label: "Búsqueda", Focused: c.section == configSectionSearch},
		settingsButtonSpec{ID: "section:security", Label: "Seguridad", Focused: c.section == configSectionSecurity},
	)
}

func (c *ConfigScreen) sectionSubtitle() string {
	switch c.section {
	case configSectionSearch:
		return "Motores de búsqueda y fuentes externas."
	case configSectionSecurity:
		return "Controles de seguridad y permisos."
	default:
		return "Preferencias generales de Lilith."
	}
}

func (c *ConfigScreen) developmentDescription() string {
	if c.section == configSectionSearch {
		return "La configuración de motores de búsqueda se añadirá en esta sección."
	}
	return "Los controles de seguridad se añadirán en esta sección."
}

func (c *ConfigScreen) skillsFocusCard(width int) settingsBlock {
	s := c.ctx.Styles
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	state := "OFF"
	stateStyle := s.Muted
	if c.settings.SkillsEnabled {
		state = "ON"
		stateStyle = s.Success
	}
	marker := "  "
	if c.focus == "skills" {
		marker = "› "
	}
	title := marker + s.Title.Render("Skills")
	status := stateStyle.Render("[" + state + "]")
	gap := inner - lipgloss.Width(title) - lipgloss.Width(status)
	if gap < 2 {
		gap = 2
	}
	lines := []string{
		title + strings.Repeat(" ", gap) + status,
		s.Muted.Render(settingsWrapPlain(fmt.Sprintf("%d skill(s) detectada(s). Activa o desactiva su carga automática.", len(c.loaded)), inner)),
	}
	style := lipgloss.NewStyle().
		Width(inner).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.Theme.Border)
	if c.focus == "skills" {
		style = style.
			BorderForeground(s.Theme.BorderFocus).
			Background(s.Theme.SurfaceHover).
			Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{
		text: text,
		hits: []settingsHit{{id: "skills", rect: settingsRect{w: lipgloss.Width(text), h: lipgloss.Height(text)}}},
	}
}

func (c *ConfigScreen) backFocusCard(width int) settingsBlock {
	s := c.ctx.Styles
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	marker := "  "
	if c.focus == "back" {
		marker = "› "
	}
	lines := []string{
		marker + s.Title.Render("Volver al chat"),
		s.Muted.Render("Regresa a la conversación actual."),
	}
	style := lipgloss.NewStyle().
		Width(inner).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.Theme.Border)
	if c.focus == "back" {
		style = style.
			BorderForeground(s.Theme.BorderFocus).
			Background(s.Theme.SurfaceHover).
			Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{
		text: text,
		hits: []settingsHit{{id: "back", rect: settingsRect{w: lipgloss.Width(text), h: lipgloss.Height(text)}}},
	}
}

func (c *ConfigScreen) View() string {
	view, _ := c.layout()
	return view
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
