package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpScreen is the compact in-terminal reference for commands and keyboard
// shortcuts. Keeping this out of the chat transcript lets the permanent status
// bar stay minimal without hiding discoverability.
type HelpScreen struct {
	ctx      *AppContext
	viewport viewport.Model
	width    int
	height   int
}

func NewHelpScreen(ctx *AppContext) *HelpScreen {
	w, h := ctx.Width, ctx.Height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	m := &HelpScreen{ctx: ctx, width: w, height: h, viewport: viewport.New(w, maxInt(1, h-4))}
	m.sync()
	return m
}

func (m *HelpScreen) Init() tea.Cmd { return tea.WindowSize() }

func (m *HelpScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.sync()
		return m, nil
	case tea.KeyMsg:
		switch v.String() {
		case "esc", "q":
			return m, switchToChat()
		case "home":
			m.viewport.GotoTop()
			return m, nil
		case "end":
			m.viewport.GotoBottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *HelpScreen) sync() {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	m.viewport.Width = maxInt(1, w-2)
	m.viewport.Height = maxInt(1, h-4)
	m.viewport.SetContent(m.content(maxInt(20, w-4)))
}

func (m *HelpScreen) content(width int) string {
	s := m.ctx.Styles
	section := lipgloss.NewStyle().Foreground(s.Theme.Primary).Bold(true)
	key := lipgloss.NewStyle().Foreground(s.Theme.Secondary).Bold(true)
	muted := s.Muted
	var b strings.Builder

	b.WriteString(section.Render("Comandos"))
	b.WriteString("\n\n")
	for _, cmd := range Commands() {
		usage := "/" + cmd.Name
		if cmd.Usage != "" {
			usage += " " + cmd.Usage
		}
		line := key.Render(usage)
		if len(cmd.Aliases) > 0 {
			aliases := make([]string, 0, len(cmd.Aliases))
			for _, alias := range cmd.Aliases {
				aliases = append(aliases, "/"+alias)
			}
			line += muted.Render("  alias: " + strings.Join(aliases, ", "))
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString("  " + wrapHelpText(cmd.Description, width-2) + "\n\n")
	}

	b.WriteString(section.Render("Atajos"))
	b.WriteString("\n\n")
	shortcuts := [][2]string{
		{"Tab / Shift+Tab", "Alternar el agente primario Build / Plan para el siguiente turno."},
		{"Enter", "Enviar. Durante una tarea agrega una instrucción de steering para la siguiente frontera segura."},
		{"Alt+Enter", "Agregar un follow-up que se ejecuta después del trabajo actual."},
		{"Shift+Enter / Ctrl+Enter", "Insertar una nueva línea en el editor."},
		{"Esc", "Abortar el turno activo; en pantallas secundarias volver al chat."},
		{"Alt+↑", "Recuperar la cola pendiente al editor sin cancelar la tarea."},
		{"Ctrl+O", "Expandir o plegar el panel de archivo/herramienta seleccionado."},
		{"Ctrl+J / Ctrl+K", "Mover la selección entre paneles de archivo/herramienta."},
		{"Ctrl+R", "Expandir o plegar el último bloque de razonamiento."},
		{"PgUp / PgDn", "Desplazar el transcript una página."},
		{"Home / End", "Ir al inicio / final del transcript."},
		{"Shift+↑ / Shift+↓", "Desplazar el transcript por líneas."},
		{"Ctrl+U / Ctrl+D", "Desplazar media página."},
		{"Ctrl+B / Ctrl+F", "Desplazar una página atrás / adelante."},
		{"!comando", "Ejecutar shell directamente. En Plan sólo se permite la allowlist de inspección."},
		{"/skills:nombre", "Invocar explícitamente una skill compatible."},
		{"Ctrl+C / Ctrl+Z", "No cierran ni suspenden Lilith. Usa /exit."},
	}
	for _, item := range shortcuts {
		b.WriteString(key.Render(item[0]))
		b.WriteString("\n  " + wrapHelpText(item[1], width-2) + "\n\n")
	}
	return strings.TrimSpace(b.String())
}

func wrapHelpText(text string, width int) string {
	if width < 20 {
		width = 20
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if lipgloss.Width(line)+1+lipgloss.Width(word) > width {
			lines = append(lines, line)
			line = word
		} else {
			line += " " + word
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n  ")
}

func (m *HelpScreen) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	title := m.ctx.Styles.Title.Foreground(m.ctx.Styles.Theme.Primary).Render("Ayuda de Lilith")
	subtitle := m.ctx.Styles.Muted.Render("Comandos y atajos disponibles")
	footer := m.ctx.Styles.Muted.Render(fmt.Sprintf("↑↓/PgUp/PgDn navegar · Esc volver  %d%%", int(m.viewport.ScrollPercent()*100)))
	return lipgloss.NewStyle().Padding(0, 1).Render(title + "\n" + subtitle + "\n\n" + m.viewport.View() + "\n" + footer)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
