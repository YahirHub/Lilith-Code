package tui

import (
	"fmt"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

type configSection string

const (
	configSectionGeneral  configSection = "general"
	configSectionSkills   configSection = "skills"
	configSectionSearch   configSection = "search"
	configSectionSecurity configSection = "security"
)

const configSectionNavFocus = "section-nav"

var configSections = []configSection{
	configSectionGeneral,
	configSectionSkills,
	configSectionSearch,
	configSectionSecurity,
}

// ConfigScreen is the interactive `/config` screen. The top section picker is
// a real focus region: horizontal keys only change sections while that region
// owns focus. Once focus moves into a section, the section can use left/right
// for its own controls without accidentally switching pages.
type ConfigScreen struct {
	ctx            *AppContext
	settings       config.Settings
	section        configSection
	focus          string
	message        string
	danger         string
	loaded         []skills.Skill
	search         *searchConfigState
	viewportOffset int
}

func NewConfigScreen(ctx *AppContext) *ConfigScreen {
	s, _ := config.Load(ctx.ConfigDir)
	loaded := skills.Load(skills.DefaultLoadOptions(ctx.ConfigDir, currentProject()))
	return &ConfigScreen{
		ctx:      ctx,
		settings: s,
		loaded:   loaded,
		section:  configSectionGeneral,
		focus:    configSectionNavFocus,
		search:   newSearchConfigState(ctx),
	}
}

func (c *ConfigScreen) Init() uikit.Cmd { return nil }

func (c *ConfigScreen) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case searchTestMsg:
		c.search.applyTest(v)
		return c, nil
	case searchTestAllMsg:
		c.search.applyTestAll(v)
		return c, nil
	case uikit.MouseMsg:
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
			c.focus = configSectionNavFocus
			return c, nil
		}
		if c.section == configSectionSearch {
			if handled, cmd := c.search.handleHit(hit.id); handled {
				c.focus = "search-content"
				return c, cmd
			}
		}
		c.focus = hit.id
		if strings.HasPrefix(hit.id, "skill:") {
			return c.toggleSkill(strings.TrimPrefix(hit.id, "skill:"))
		}
		switch hit.id {
		case "skills-global":
			return c.toggleSkills()
		case "lilith-md":
			return c.toggleSetting("lilith-md")
		case "claude-md":
			return c.toggleSetting("claude-md")
		case "auto-memory":
			return c.toggleSetting("auto-memory")
		case "hooks":
			return c.toggleSetting("hooks")
		case "trusted-project":
			return c.toggleSetting("trusted-project")
		case "back":
			return c, switchToChat()
		}

	case uikit.KeyMsg:
		// Nested search screens (provider detail, API key and fallback order)
		// own their keyboard completely until they return to the provider list.
		if c.section == configSectionSearch && c.search.inNestedView() {
			if handled, cmd := c.search.handleKey(v); handled {
				return c, cmd
			}
		}

		switch v.String() {
		case "esc", "q":
			return c, switchToChat()
		}

		if c.focus == configSectionNavFocus {
			switch v.String() {
			case "tab", "right", "l":
				c.rotateSection(1)
				return c, nil
			case "shift+tab", "left", "h":
				c.rotateSection(-1)
				return c, nil
			case "down", "j", "enter":
				c.focusFirstContent()
				return c, nil
			case "up", "k":
				return c, nil
			}
			return c, nil
		}

		if c.section == configSectionSearch && c.focus == "search-content" {
			// Pressing up on the first provider deliberately returns focus to the
			// top section navigation. Horizontal keys remain owned by the search
			// content while focus is below that navigation.
			if (v.String() == "up" || v.String() == "k") && c.search.atListTop() {
				c.focus = configSectionNavFocus
				return c, nil
			}
			if handled, cmd := c.search.handleKey(v); handled {
				return c, cmd
			}
			return c, nil
		}

		switch v.String() {
		case "down", "j":
			c.moveFocus(1)
			return c, nil
		case "up", "k":
			c.moveFocus(-1)
			return c, nil
		case "enter", " ":
			if strings.HasPrefix(c.focus, "skill:") && c.section == configSectionSkills {
				return c.toggleSkill(strings.TrimPrefix(c.focus, "skill:"))
			}
			switch c.focus {
			case "skills-global":
				if c.section == configSectionSkills {
					return c.toggleSkills()
				}
			case "lilith-md", "claude-md", "auto-memory", "hooks":
				if c.section == configSectionGeneral {
					return c.toggleSetting(c.focus)
				}
			case "trusted-project":
				if c.section == configSectionSecurity {
					return c.toggleSetting(c.focus)
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
		c.focus = configSectionNavFocus
		if section == configSectionSearch && c.search != nil {
			c.search.resetToList()
			c.search.reload()
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

func (c *ConfigScreen) focusFirstContent() {
	switch c.section {
	case configSectionGeneral:
		c.focus = "lilith-md"
	case configSectionSkills:
		c.focus = "skills-global"
	case configSectionSearch:
		c.focus = "search-content"
		c.search.ensureSelectedProvider()
	case configSectionSecurity:
		c.focus = "trusted-project"
	}
}

func (c *ConfigScreen) moveFocus(delta int) {
	order := []string{"back"}
	if c.section == configSectionGeneral {
		order = []string{"lilith-md", "claude-md", "auto-memory", "hooks", "back"}
	} else if c.section == configSectionSkills {
		order = c.skillFocusOrder()
	} else if c.section == configSectionSecurity {
		order = []string{"trusted-project", "back"}
	}
	idx := 0
	for i, id := range order {
		if id == c.focus {
			idx = i
			break
		}
	}
	if delta < 0 && idx == 0 {
		c.focus = configSectionNavFocus
		return
	}
	next := idx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(order) {
		next = len(order) - 1
	}
	c.focus = order[next]
}

func (c *ConfigScreen) toggleSkills() (uikit.Model, uikit.Cmd) {
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

func (c *ConfigScreen) skillFocusOrder() []string {
	order := make([]string, 0, len(c.loaded)+2)
	order = append(order, "skills-global")
	for _, sk := range c.loaded {
		order = append(order, "skill:"+sk.Name)
	}
	return append(order, "back")
}

func (c *ConfigScreen) toggleSkill(name string) (uikit.Model, uikit.Cmd) {
	var selected *skills.Skill
	for i := range c.loaded {
		if strings.EqualFold(c.loaded[i].Name, strings.TrimSpace(name)) {
			selected = &c.loaded[i]
			break
		}
	}
	if selected == nil {
		c.danger = "Skill no encontrada: " + strings.TrimSpace(name)
		return c, nil
	}
	enabled := !config.IsSkillEnabled(c.settings, selected.Name)
	config.SetSkillEnabled(&c.settings, selected.Name, enabled)
	if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
		c.danger = "No se pudo guardar: " + err.Error()
		return c, nil
	}
	c.danger = ""
	state := "desactivada"
	if enabled {
		state = "activada"
	}
	c.message = fmt.Sprintf("Skill %s %s.", selected.Name, state)
	return c, nil
}

func (c *ConfigScreen) enabledSkillCount() int {
	count := 0
	for _, sk := range c.loaded {
		if config.IsSkillEnabled(c.settings, sk.Name) {
			count++
		}
	}
	return count
}

func (c *ConfigScreen) layout() (string, []settingsHit) {
	view, hits := c.fullLayout()
	return c.visibleLayout(view, hits)
}

func (c *ConfigScreen) fullLayout() (string, []settingsHit) {
	s := c.ctx.Styles
	w := settingsContentWidth(c.ctx.Width)
	canvas := newSettingsCanvas(w)
	canvas.block(settingsHeader(s, "Configuración", c.sectionSubtitle()))
	canvas.blank()

	searchNested := c.section == configSectionSearch && c.search != nil && c.search.inNestedView()
	if !searchNested {
		canvas.block(c.sectionPicker(w))
		canvas.blank()
	}

	switch c.section {
	case configSectionGeneral:
		canvas.block(c.toggleFocusCard(w, "lilith-md", "Lilith / LILITH.md", "Carga LILITH.md o LI.md del usuario y del proyecto como instrucciones persistentes.", c.settings.ProjectInstructionsEnabled))
		canvas.blank()
		canvas.block(c.toggleFocusCard(w, "claude-md", "Claude / CLAUDE.md", "Compatibilidad con CLAUDE.md, CLAUDE.local.md, .claude/rules y .claude/commands.", c.settings.ClaudeCompatibilityEnabled))
		canvas.blank()
		canvas.block(c.toggleFocusCard(w, "auto-memory", "Memoria automática", "Carga memoria persistente de Lilith y memoria declarada por subagentes.", c.settings.AutoMemoryEnabled))
		canvas.blank()
		canvas.block(c.toggleFocusCard(w, "hooks", "Hooks Claude", "Permite hooks compatibles de settings para validar o reaccionar a eventos del agente.", c.settings.HooksEnabled))
		canvas.blank()
		canvas.block(settingsCard(s, settingsCardSpec{
			Title:       "Ruta de configuración",
			Description: c.ctx.ConfigDir,
			Badge:       "INFO",
			Width:       w,
		}))
		canvas.blank()
		canvas.block(c.backFocusCard(w))
	case configSectionSkills:
		canvas.block(c.skillsGlobalFocusCard(w))
		for _, sk := range c.loaded {
			canvas.blank()
			canvas.block(c.skillToggleFocusCard(w, sk))
		}
		canvas.blank()
		canvas.block(c.backFocusCard(w))
	case configSectionSearch:
		c.search.appendLayout(canvas, w, c.focus == "search-content")
	case configSectionSecurity:
		trusted := config.IsProjectTrusted(c.settings, currentProject())
		canvas.block(c.toggleFocusCard(w, "trusted-project", "Proyecto confiable", "Autoriza hooks y configuración ejecutable de .claude para este proyecto. Desactívalo si no confías en el repositorio.", trusted))
		canvas.blank()
		canvas.block(settingsCard(s, settingsCardSpec{
			Title:       "Hooks de proyecto",
			Description: "Los hooks de ~/.claude pueden ejecutarse sin esta autorización; los hooks dentro del repositorio requieren Proyecto confiable.",
			Badge:       "SEGURIDAD",
			Width:       w,
		}))
		canvas.blank()
		canvas.block(c.backFocusCard(w))
	}

	if c.message != "" && c.section != configSectionSearch {
		canvas.blank()
		canvas.line(s.Success.Render("· " + settingsWrapPlain(c.message, w)))
	}
	if c.danger != "" {
		canvas.blank()
		canvas.line(s.Danger.Render("Error: " + settingsWrapPlain(c.danger, w)))
	}
	if c.section != configSectionSearch {
		canvas.blank()
		canvas.block(settingsFooter(s, c.footerText()))
	}
	return canvas.render(c.ctx.Width)
}

// visibleLayout constrains /config to the physical terminal height and keeps
// the currently focused control inside that window. The full settings layout
// remains intact; only viewportOffset changes while the user navigates.
func (c *ConfigScreen) visibleLayout(view string, hits []settingsHit) (string, []settingsHit) {
	lines := strings.Split(view, "\n")
	height := c.ctx.Height
	if height <= 0 || len(lines) <= height {
		c.viewportOffset = 0
		return view, hits
	}

	maxOffset := len(lines) - height
	focusID, forceTop := c.focusedHitID()
	if forceTop {
		c.viewportOffset = 0
	} else if focusID != "" {
		for _, hit := range hits {
			if hit.id != focusID {
				continue
			}
			if hit.rect.y < c.viewportOffset {
				c.viewportOffset = hit.rect.y
			}
			if bottom := hit.rect.y + hit.rect.h; bottom > c.viewportOffset+height {
				c.viewportOffset = bottom - height
			}
			break
		}
	}
	if c.viewportOffset < 0 {
		c.viewportOffset = 0
	}
	if c.viewportOffset > maxOffset {
		c.viewportOffset = maxOffset
	}

	start := c.viewportOffset
	end := start + height
	visibleHits := make([]settingsHit, 0, len(hits))
	for _, hit := range hits {
		top := hit.rect.y
		bottom := hit.rect.y + hit.rect.h
		if bottom <= start || top >= end {
			continue
		}
		if top < start {
			top = start
		}
		if bottom > end {
			bottom = end
		}
		hit.rect.y = top - start
		hit.rect.h = bottom - top
		visibleHits = append(visibleHits, hit)
	}
	return strings.Join(lines[start:end], "\n"), visibleHits
}

// focusedHitID maps the logical focus used by /config to the concrete block
// that must remain visible. Returning forceTop for the section navigation also
// guarantees that repeated Up really returns to the header instead of leaving
// the viewport anchored to a lower card.
func (c *ConfigScreen) focusedHitID() (id string, forceTop bool) {
	if c.focus == configSectionNavFocus {
		return "section:" + string(c.section), true
	}
	if c.section != configSectionSearch || c.focus != "search-content" || c.search == nil {
		return c.focus, false
	}

	switch c.search.view {
	case searchViewProvider:
		return c.search.focus, false
	case searchViewKey:
		return "search-key-input", false
	case searchViewOrder:
		if c.search.orderAt >= 0 && c.search.orderAt < len(c.search.order) {
			return "search-order:" + string(c.search.order[c.search.orderAt]), false
		}
	case searchViewList:
		return "search-provider:" + string(c.search.selected), false
	}
	return "", false
}

func (c *ConfigScreen) sectionPicker(width int) settingsBlock {
	return settingsButtonGroup(c.ctx.Styles, width,
		settingsButtonSpec{ID: "section:general", Label: "General", Active: c.section == configSectionGeneral, Focused: c.section == configSectionGeneral && c.focus == configSectionNavFocus},
		settingsButtonSpec{ID: "section:skills", Label: "Skills", Active: c.section == configSectionSkills, Focused: c.section == configSectionSkills && c.focus == configSectionNavFocus},
		settingsButtonSpec{ID: "section:search", Label: "Búsqueda", Active: c.section == configSectionSearch, Focused: c.section == configSectionSearch && c.focus == configSectionNavFocus},
		settingsButtonSpec{ID: "section:security", Label: "Seguridad", Active: c.section == configSectionSecurity, Focused: c.section == configSectionSecurity && c.focus == configSectionNavFocus},
	)
}

func (c *ConfigScreen) sectionSubtitle() string {
	if c.section == configSectionSearch && c.search != nil && c.search.inNestedView() {
		return c.search.nestedSubtitle()
	}
	switch c.section {
	case configSectionSkills:
		return "Habilidades reutilizables y metodología interna."
	case configSectionSearch:
		return "Motores de búsqueda y fuentes externas."
	case configSectionSecurity:
		return "Controles de seguridad y permisos."
	default:
		return "Preferencias generales de Lilith."
	}
}

func (c *ConfigScreen) footerText() string {
	if c.focus == configSectionNavFocus {
		return "←→ cambiar sección · ↓ entrar · clic · Esc volver"
	}
	return "↑↓ mover foco · ↑ hasta la barra superior · Enter/Espacio usar · clic · Esc volver"
}

func (c *ConfigScreen) toggleSetting(id string) (uikit.Model, uikit.Cmd) {
	switch id {
	case "lilith-md":
		c.settings.ProjectInstructionsEnabled = !c.settings.ProjectInstructionsEnabled
	case "claude-md":
		c.settings.ClaudeCompatibilityEnabled = !c.settings.ClaudeCompatibilityEnabled
	case "auto-memory":
		c.settings.AutoMemoryEnabled = !c.settings.AutoMemoryEnabled
	case "hooks":
		c.settings.HooksEnabled = !c.settings.HooksEnabled
	case "trusted-project":
		config.SetProjectTrusted(&c.settings, currentProject(), !config.IsProjectTrusted(c.settings, currentProject()))
	default:
		return c, nil
	}
	if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
		c.danger = "No se pudo guardar: " + err.Error()
		return c, nil
	}
	c.danger = ""
	c.message = "Configuración actualizada."
	return c, nil
}

func (c *ConfigScreen) toggleFocusCard(width int, id, title, description string, enabled bool) settingsBlock {
	s := c.ctx.Styles
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	marker := "  "
	if c.focus == id {
		marker = "› "
	}
	state := "OFF"
	stateStyle := s.Muted
	if enabled {
		state = "ON"
		stateStyle = s.Success
	}
	titleText := marker + s.Title.Render(title)
	status := stateStyle.Render("[" + state + "]")
	gap := inner - tuistyle.Width(titleText) - tuistyle.Width(status)
	if gap < 2 {
		gap = 2
	}
	lines := []string{titleText + strings.Repeat(" ", gap) + status, s.Muted.Render(settingsWrapPlain(description, inner))}
	style := tuistyle.NewStyle().Width(inner).Padding(0, 1).Border(tuistyle.RoundedBorder()).BorderForeground(s.Theme.Border)
	if c.focus == id {
		style = style.BorderForeground(s.Theme.BorderFocus).Background(s.Theme.SurfaceHover).Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{text: text, hits: []settingsHit{{id: id, rect: settingsRect{w: tuistyle.Width(text), h: tuistyle.Height(text)}}}}
}

func (c *ConfigScreen) skillsGlobalFocusCard(width int) settingsBlock {
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
	if c.focus == "skills-global" {
		marker = "› "
	}
	title := marker + s.Title.Render("Skills")
	status := stateStyle.Render("[" + state + "]")
	gap := inner - tuistyle.Width(title) - tuistyle.Width(status)
	if gap < 2 {
		gap = 2
	}
	lines := []string{
		title + strings.Repeat(" ", gap) + status,
		s.Muted.Render(settingsWrapPlain(fmt.Sprintf("Interruptor maestro. %d de %d skill(s) habilitada(s) individualmente.", c.enabledSkillCount(), len(c.loaded)), inner)),
	}
	style := tuistyle.NewStyle().
		Width(inner).
		Padding(0, 1).
		Border(tuistyle.RoundedBorder()).
		BorderForeground(s.Theme.Border)
	if c.focus == "skills-global" {
		style = style.
			BorderForeground(s.Theme.BorderFocus).
			Background(s.Theme.SurfaceHover).
			Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{
		text: text,
		hits: []settingsHit{{id: "skills-global", rect: settingsRect{w: tuistyle.Width(text), h: tuistyle.Height(text)}}},
	}
}

func (c *ConfigScreen) skillToggleFocusCard(width int, sk skills.Skill) settingsBlock {
	s := c.ctx.Styles
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	id := "skill:" + sk.Name
	enabled := config.IsSkillEnabled(c.settings, sk.Name)
	state := "OFF"
	stateStyle := s.Muted
	if enabled {
		state = "ON"
		stateStyle = s.Success
	}
	marker := "  "
	if c.focus == id {
		marker = "› "
	}
	title := marker + s.Title.Render(sk.Name)
	status := stateStyle.Render("[" + state + "]")
	gap := inner - tuistyle.Width(title) - tuistyle.Width(status)
	if gap < 2 {
		gap = 2
	}
	description := strings.TrimSpace(sk.Description)
	if description == "" {
		description = "Skill sin descripción."
	}
	description += " · origen: " + skillSourceLabel(sk.Source)
	if !c.settings.SkillsEnabled {
		description += " · interruptor maestro OFF"
	}
	lines := []string{
		title + strings.Repeat(" ", gap) + status,
		s.Muted.Render(settingsWrapPlain(description, inner)),
	}
	style := tuistyle.NewStyle().Width(inner).Padding(0, 1).Border(tuistyle.RoundedBorder()).BorderForeground(s.Theme.Border)
	if c.focus == id {
		style = style.BorderForeground(s.Theme.BorderFocus).Background(s.Theme.SurfaceHover).Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{text: text, hits: []settingsHit{{id: id, rect: settingsRect{w: tuistyle.Width(text), h: tuistyle.Height(text)}}}}
}

func skillSourceLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "builtin":
		return "interna"
	case "project":
		return "proyecto"
	case "user":
		return "usuario"
	default:
		return "externa"
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
	style := tuistyle.NewStyle().
		Width(inner).
		Padding(0, 1).
		Border(tuistyle.RoundedBorder()).
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
		hits: []settingsHit{{id: "back", rect: settingsRect{w: tuistyle.Width(text), h: tuistyle.Height(text)}}},
	}
}

func (c *ConfigScreen) View() string {
	view, _ := c.layout()
	return view
}

var _ uikit.Model = (*ConfigScreen)(nil)

// currentProject wraps os.Getwd() para poder ser stubbeado en tests.
func currentProject() string {
	dir, err := getCwd()
	if err != nil {
		return "."
	}
	return dir
}
