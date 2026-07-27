package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"


	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/skills"
	"github.com/lilith/li/internal/tools"
)

// MessageKind classifies rendered chat messages.
type MessageKind int

const (
	MsgUser MessageKind = iota
	MsgAssistant
	MsgSystem
	MsgError
	MsgTool
	MsgFile
	MsgCommand
	MsgThinking
)

// ChatMessage is one rendered entry in the transcript.
type ChatMessage struct {
	Kind    MessageKind
	Content string
	Time    time.Time
	// Panel se usa en MsgFile: ventana plegable con el contenido en vivo.
	Panel *FilePanel
	// Command se usa en MsgCommand: ventana estilo terminal para
	// run_terminal_command con estado (azul/verde/rojo) y streaming.
	Command *CommandPanel
	// Thinking se usa en MsgThinking: resumen de razonamiento del modelo,
	// con toggle expandir/plegar y alto fijo en vista previa.
	Thinking *ThinkingPanel
}

// InputMode is the state of the input bar (default | bash).
type InputMode string

const (
	ModeDefault InputMode = "default"
	ModeBash    InputMode = "bash"
)

// ChatModel is the main chat screen. Used via pointer.
type ChatModel struct {
	ctx       *AppContext
	viewport  viewport.Model
	textarea  textarea.Model
	messages  []ChatMessage
	mode      InputMode
	streaming bool
	streamBuf strings.Builder
	cancel    context.CancelFunc

	// history is the real conversation sent to the model (incluye mensajes
	// de herramienta), separada del transcript que se dibuja en pantalla.
	history      []openai.Message
	activeTools  []string
	pendingCall  []openai.ToolCall
	toolSteps    int
	toolFallback string

	// Persistencia de la conversación (historial por proyecto).
	store   *session.Store
	sess    *session.Session
	project string
	// seen marca los archivos leídos/escritos: evita reescrituras a ciegas.
	seen map[string]bool

	// Paneles de archivo en vivo (creación/edición con diff plegable).
	livePanels  map[int]*FilePanel
	panelByCall map[string]*FilePanel
	panelSel    int
	// panelPinned indica que el usuario eligió un panel con ctrl+j/k. Mientras
	// sea falso, ctrl+o actúa siempre sobre la última ventana abierta.
	panelPinned bool
	// Paneles de comando en vivo (run_terminal_command con streaming).
	cmdPanels  map[int]*CommandPanel
	cmdByCall  map[string]*CommandPanel

	thinkingFrame int
	thinking      bool
	// working indica que hay un lote de herramientas ejecutándose. Se
	// pinta con un shimmer verde ("Trabajando…") para distinguirlo del
	// estado "Pensando…" (esperando tokens del modelo). Cuando ambos son
	// false, el turno está detenido.
	working bool

	// thinkingActive apunta al panel de "pensamiento" del turno actual (si
	// el modelo emite reasoning summary). Se resetea al iniciar el turno.
	thinkingActive *ThinkingPanel

	// quitPrimedAt marca el momento en que el usuario pulsó Ctrl+C sin
	// tarea activa. Un segundo Ctrl+C dentro de 2s confirma la salida.
	quitPrimedAt time.Time

	paletteOpen bool
	paletteIdx  int
	paletteRows []SlashCommand

	// userScrolled es true cuando el usuario desplazó el transcript hacia
	// arriba manualmente. Mientras lo esté, el auto-scroll a fondo queda
	// desactivado para poder leer historial aunque Lilith siga trabajando.
	userScrolled bool
	// cmdTickActive evita programar varios timers de "Elapsed" en paralelo.
	cmdTickActive bool

	// queue guarda los mensajes que el usuario envió mientras Lilith ya
	// estaba trabajando. Se drenan uno a uno cuando termina el turno actual.
	// Sólo se ejecuta una tarea a la vez; Ctrl+C cancela la activa y limpia
	// la cola pendiente.
	queue []string

	// lastKeyAt guarda el instante del último KeyMsg que no sea Enter.
	// Sirve como heurística de "paste" en terminales que no envían
	// bracketed paste (p.ej. el host clásico de PowerShell en Windows):
	// si un Enter llega < 25ms tras la tecla anterior se trata como salto
	// de línea dentro del textarea en vez de enviar el mensaje.
	lastKeyAt time.Time
}

// chatStreamMsg is emitted by the streaming pump for each SSE chunk.
type chatStreamMsg struct {
	ch           <-chan openai.Chunk
	delta        string
	toolCalls    []openai.ToolCall
	superseded   []int
	partial      bool
	thinking     string
	thinkingDone bool
	done         bool
	err          error
}

// toolResultsMsg carries the outcome of a batch of tool calls.
type toolResultsMsg struct {
	results []openai.Message
	err     error
}

// cmdElapsedTickMsg refresca el transcript a intervalo fijo mientras haya
// paneles de comando en ejecución, para que la línea "Elapsed …" avance de
// forma suave (antes dependía de que llegara delta streaming y saltaba a
// tirones).
type cmdElapsedTickMsg struct{}

func cmdElapsedTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return cmdElapsedTickMsg{} })
}

// hasRunningCommand devuelve true si algún CommandPanel sigue vivo.
func (m *ChatModel) hasRunningCommand() bool {
	for _, cp := range m.cmdPanels {
		if cp != nil && !cp.Done {
			return true
		}
	}
	return false
}

// maybeStartElapsedTick arranca el timer de elapsed una única vez.
func (m *ChatModel) maybeStartElapsedTick() tea.Cmd {
	if m.cmdTickActive {
		return nil
	}
	m.cmdTickActive = true
	return cmdElapsedTick()
}

// isScrollKey devuelve true para teclas que deben mover el viewport en vez
// de ir al textarea, incluso mientras Lilith está trabajando.
func isScrollKey(k string) bool {
	switch k {
	case "pgup", "pgdown", "home", "end",
		"shift+up", "shift+down",
		"ctrl+u", "ctrl+d",
		"ctrl+b", "ctrl+f":
		return true
	}
	return false
}

func NewChat(ctx *AppContext) ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Escribe un mensaje…   ( / comandos · ! bash · Enter enviar · Enter en tarea = encolar · Ctrl+C cancela/limpia cola · Ctrl+C x2 salir )"
	ta.Prompt = "❯ "
	ta.CharLimit = 20_000
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.MaxHeight = 8
	ta.Focus()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Primary)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Foreground)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Muted)

	vp := viewport.New(80, 20)
	vp.SetContent("")

	project, _ := os.Getwd()
	m := ChatModel{
		ctx:      ctx,
		viewport: vp,
		textarea: ta,
		mode:     ModeDefault,
		store:    session.NewStore(ctx.ConfigDir),
		project:  project,
		sess:     session.New(project),
		seen:     map[string]bool{},
	}
	return m
}

// persist guarda la conversación actual (silencioso: la persistencia nunca
// debe romper el turno del usuario).
func (m *ChatModel) persist() {
	if m.store == nil || m.sess == nil {
		return
	}
	m.sess.Messages = m.history
	_ = m.store.Save(m.sess)
}

// LoadSession reemplaza la conversación activa por una guardada y reconstruye
// el transcript visible a partir del historial real. Las tool calls de
// archivo (write_file / str_replace) se rehidratan como FilePanel para que
// la sesión reanudada conserve el mismo diseño que cuando se estaban
// ejecutando en vivo, en lugar de degradarse a una línea de texto genérica.
func (m *ChatModel) LoadSession(s *session.Session) {
	if s == nil {
		return
	}
	m.Clear()
	m.sess = s
	m.history = s.Messages
	m.livePanels = map[int]*FilePanel{}
	m.panelByCall = map[string]*FilePanel{}
	m.cmdPanels = map[int]*CommandPanel{}
	m.cmdByCall = map[string]*CommandPanel{}
	// Índices temporales por CallID para poder emparejar el resultado (rol
	// "tool") con el panel que lo generó al recorrer el historial.
	panelByID := map[string]*FilePanel{}
	cmdByID := map[string]*CommandPanel{}
	nextPanelIdx := 0
	for _, msg := range s.Messages {
		switch msg.Role {
		case "user":
			m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: msg.Content, Time: s.UpdatedAt})
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: msg.Content, Time: s.UpdatedAt})
			}
			for _, c := range msg.ToolCalls {
				switch {
				case IsFileTool(c.Function.Name):
					p := &FilePanel{Tool: c.Function.Name, Index: nextPanelIdx, CallID: c.ID}
					nextPanelIdx++
					p.Update(c.Function.Arguments)
					m.livePanels[p.Index] = p
					if c.ID != "" {
						m.panelByCall[c.ID] = p
						panelByID[c.ID] = p
					}
					m.messages = append(m.messages, ChatMessage{Kind: MsgFile, Panel: p, Time: s.UpdatedAt})
				case IsCommandTool(c.Function.Name):
					cp := &CommandPanel{Index: nextPanelIdx, CallID: c.ID}
					nextPanelIdx++
					cp.Update(c.Function.Arguments)
					m.cmdPanels[cp.Index] = cp
					if c.ID != "" {
						m.cmdByCall[c.ID] = cp
						cmdByID[c.ID] = cp
					}
					m.messages = append(m.messages, ChatMessage{Kind: MsgCommand, Command: cp, Time: s.UpdatedAt})
				default:
					m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: describeCall(c), Time: s.UpdatedAt})
				}
			}
		case "tool":
			if p := panelByID[msg.ToolCallID]; p != nil {
				p.Finish(msg.Content)
				continue
			}
			if cp := cmdByID[msg.ToolCallID]; cp != nil {
				cp.Finish(msg.Content)
				continue
			}
			m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: "  ↳ " + firstLine(msg.Content), Time: s.UpdatedAt})
		}
	}
	// Cualquier panel sin resultado (turno cortado) se muestra como
	// "reintentado" en vez de dejar el shimmer para siempre.
	for _, p := range m.livePanels {
		if !p.Done {
			p.MarkSuperseded()
		}
	}
	for _, cp := range m.cmdPanels {
		if !cp.Done {
			cp.MarkSuperseded()
		}
	}
	if panels := m.panels(); len(panels) > 0 {
		m.panelSel = len(panels) - 1
	}
	m.AddSystem("Sesión reanudada: " + s.Title)
	m.refreshTranscript(true)
}

func (m *ChatModel) Resize(w, h int) {
	m.ctx.Width, m.ctx.Height = w, h
	if w > 10 {
		// ancho útil = terminal − 2 bordes − 2 padding
		m.textarea.SetWidth(w - 4)
	}
	m.setInputHeightForContent()
	vpHeight := h - m.bottomChromeHeight(w)
	if vpHeight < 3 {
		vpHeight = 3
	}
	// Reservamos 1 columna a la derecha para la scrollbar vertical.
	vpWidth := w - 1
	if vpWidth < 10 {
		vpWidth = w
	}
	m.viewport.Width = vpWidth
	m.viewport.Height = vpHeight
	m.refreshTranscript(true)
}

func (m *ChatModel) AddSystem(text string) {
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: text, Time: time.Now()})
	m.refreshTranscript(true)
}

func (m *ChatModel) AddError(text string) {
	m.messages = append(m.messages, ChatMessage{Kind: MsgError, Content: text, Time: time.Now()})
	m.refreshTranscript(true)
}

func (m *ChatModel) Clear() {
	m.messages = nil
	m.history = nil
	m.activeTools = nil
	m.pendingCall = nil
	m.toolSteps = 0
	m.toolFallback = ""
	m.livePanels = nil
	m.panelByCall = nil
	m.cmdPanels = nil
	m.cmdByCall = nil
	m.panelSel = 0
	m.panelPinned = false
	m.thinkingActive = nil
	m.seen = map[string]bool{}
	m.sess = session.New(m.project)
	m.refreshTranscript(false)
}

// panels devuelve los paneles de archivo en orden de aparición.
func (m *ChatModel) panels() []*FilePanel {
	var out []*FilePanel
	for _, msg := range m.messages {
		if msg.Kind == MsgFile && msg.Panel != nil {
			out = append(out, msg.Panel)
		}
	}
	return out
}

// lastAssistantIndex localiza la burbuja del asistente en curso.
func (m *ChatModel) lastAssistantIndex() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Kind == MsgAssistant {
			return i
		}
	}
	return -1
}

// applyToolCalls crea o refresca las ventanas en vivo (archivo o comando)
// con los argumentos (posiblemente parciales) que el modelo lleva emitidos.
func (m *ChatModel) applyToolCalls(calls []openai.ToolCall) {
	if m.livePanels == nil {
		m.livePanels = map[int]*FilePanel{}
	}
	if m.panelByCall == nil {
		m.panelByCall = map[string]*FilePanel{}
	}
	if m.cmdPanels == nil {
		m.cmdPanels = map[int]*CommandPanel{}
	}
	if m.cmdByCall == nil {
		m.cmdByCall = map[string]*CommandPanel{}
	}
	for _, c := range calls {
		switch {
		case IsFileTool(c.Function.Name):
			var p *FilePanel
			if c.ID != "" {
				p = m.panelByCall[c.ID]
			}
			if p == nil {
				if existing := m.livePanels[c.Index]; existing != nil && (existing.CallID == "" || existing.CallID == c.ID) && !existing.Done {
					p = existing
				}
			}
			if p == nil {
				// Red de seguridad: cualquier panel previo aún abierto se
				// marca como reintentado.
				for idx, prev := range m.livePanels {
					if idx == c.Index || prev == nil || prev.Done {
						continue
					}
					prev.MarkSuperseded()
				}
				for idx, prev := range m.cmdPanels {
					if idx == c.Index || prev == nil || prev.Done {
						continue
					}
					prev.MarkSuperseded()
				}
				p = &FilePanel{Tool: c.Function.Name, Index: c.Index}
				m.livePanels[c.Index] = p
				m.messages = append(m.messages, ChatMessage{Kind: MsgFile, Panel: p, Time: time.Now()})
				if !m.panelPinned {
					m.panelSel = len(m.panels()) - 1
				}
			}
			m.livePanels[c.Index] = p
			if c.ID != "" {
				p.CallID = c.ID
				m.panelByCall[c.ID] = p
			}
			p.Update(c.Function.Arguments)
		case IsCommandTool(c.Function.Name):
			var cp *CommandPanel
			if c.ID != "" {
				cp = m.cmdByCall[c.ID]
			}
			if cp == nil {
				if existing := m.cmdPanels[c.Index]; existing != nil && (existing.CallID == "" || existing.CallID == c.ID) && !existing.Done {
					cp = existing
				}
			}
			if cp == nil {
				for idx, prev := range m.livePanels {
					if idx == c.Index || prev == nil || prev.Done {
						continue
					}
					prev.MarkSuperseded()
				}
				for idx, prev := range m.cmdPanels {
					if idx == c.Index || prev == nil || prev.Done {
						continue
					}
					prev.MarkSuperseded()
				}
				cp = &CommandPanel{Index: c.Index}
				m.messages = append(m.messages, ChatMessage{Kind: MsgCommand, Command: cp, Time: time.Now()})
			}
			m.cmdPanels[c.Index] = cp
			if c.ID != "" {
				cp.CallID = c.ID
				m.cmdByCall[c.ID] = cp
			}
			cp.Update(c.Function.Arguments)
		}
	}
}

// renderHeader dibuja el bloque superior fijo estilo Codewolf: logo compacto,
// una línea descriptiva y la ruta del proyecto en `muted`.
func (m *ChatModel) renderHeader() string {
	s := m.ctx.Styles
	w := m.ctx.Width
	if w <= 0 {
		w = 80
	}

	cwd, _ := os.Getwd()
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, cwd); err == nil && !strings.HasPrefix(rel, "..") {
			cwd = "~/" + rel
		}
	}

	logo := RenderLogo(w, 12, s.Theme)
	tag := s.Subtitle.Render("Lilith ejecutará comandos en tu nombre para ayudarte a construir.")
	dir := s.Muted.Render("Directorio ") + lipgloss.NewStyle().Foreground(s.Theme.Primary).Render(cwd)
	return lipgloss.JoinVertical(lipgloss.Left, logo, "", tag, dir, "")
}

func (m *ChatModel) renderTranscript() string {
	s := m.ctx.Styles
	w := m.ctx.Width - 4
	if w < 20 {
		w = 60
	}
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	for i, msg := range m.messages {
		ts := msg.Time.Format("15:04")
		stamp := s.Muted.Render("[" + ts + "]")
		switch msg.Kind {
		case MsgUser:
			b.WriteString(stamp + " " + s.MessageUser.Render("tú"))
			b.WriteString("\n")
			b.WriteString(indent(msg.Content, "  "))
		case MsgAssistant:
			b.WriteString(stamp + " " + s.Accent.Render("» lilith"))
			b.WriteString("\n")
			rendered := RenderMarkdown(msg.Content, w-2)
			if rendered == "" {
				rendered = msg.Content
			}
			b.WriteString(indent(rendered, "  "))

		case MsgSystem:
			b.WriteString(s.Muted.Render("· ") + s.MessageSystem.Render(msg.Content))
		case MsgError:
			b.WriteString(s.MessageError.Render("!! ") + s.Danger.Render(msg.Content))
		case MsgTool:
			// Tool messages already start with "$ " (see describeCall) or "  ↳"
			// for results, so we render them verbatim in a muted terminal-ish
			// tone without stacking another glyph on top.
			style := lipgloss.NewStyle().Foreground(s.Theme.Muted)
			if strings.HasPrefix(msg.Content, "$ ") {
				head := s.Accent.Render("$")
				rest := style.Render(strings.TrimPrefix(msg.Content, "$"))
				b.WriteString(head + rest)
			} else {
				b.WriteString(style.Render(msg.Content))
			}
		case MsgFile:
			selected := false
			if all := m.panels(); len(all) > 0 && m.panelSel >= 0 && m.panelSel < len(all) {
				selected = all[m.panelSel] == msg.Panel
			}
			b.WriteString(msg.Panel.View(s, w, selected))
		case MsgCommand:
			if msg.Command != nil {
				b.WriteString(msg.Command.View(s, w, false))
			}
		case MsgThinking:
			if msg.Thinking != nil {
				b.WriteString(msg.Thinking.View(s, w, false))
			}
		}
		if i < len(m.messages)-1 {
			b.WriteString("\n\n")
		}
	}
	// Nota: el shimmer "Trabajando…/Pensando…" NO se pinta aquí. Se renderiza
	// como cromo fijo (pinnedActivityView) encima del input para que no se
	// mueva con el scroll ni desaparezca cuando se anexan mensajes de tool.

	return b.String()
}


func (m *ChatModel) refreshTranscript(scrollBottom bool) {
	content := m.renderTranscript()
	if m.viewport.Width > 0 {
		content = lipgloss.NewStyle().Width(m.viewport.Width).Render(content)
	}
	m.viewport.SetContent(content)
	// Sólo hacemos autoscroll cuando el usuario no ha subido manualmente. Así
	// puede leer historial mientras la CLI sigue ejecutando comandos, sin que
	// cada frame lo tire de vuelta al fondo.
	if scrollBottom && !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func (m *ChatModel) Init() tea.Cmd { return textarea.Blink }

// visualInputLineCount estima las filas visibles que ocupará el valor dentro
// del textarea. Bubbles no expone el conteo de líneas soft-wrapped: LineCount
// sólo devuelve líneas lógicas. Por eso usamos el ancho real de texto y
// reservamos una fila extra cuando la línea cae justo en el borde, igual que el
// cursor/espacio final que renderiza textarea.
func visualInputLineCount(value string, textWidth, maxLines int) int {
	if textWidth < 1 {
		textWidth = 1
	}
	if maxLines < 1 {
		maxLines = 1
	}
	lines := 0
	for _, row := range strings.Split(value, "\n") {
		width := lipgloss.Width(row)
		rowLines := 1
		if width > 0 {
			rowLines = (width / textWidth) + 1
		}
		lines += rowLines
		if lines >= maxLines {
			return maxLines
		}
	}
	if lines < 1 {
		lines = 1
	}
	return lines
}

func (m *ChatModel) setInputHeightForContent() bool {
	maxHeight := m.textarea.MaxHeight
	if maxHeight < 1 {
		maxHeight = 8
	}
	value := m.textarea.Value()
	totalLines := visualInputLineCount(value, m.textarea.Width(), 1_000_000)
	lines := totalLines
	if lines > maxHeight {
		lines = maxHeight
	}
	if lines == m.textarea.Height() {
		return false
	}
	m.textarea.SetHeight(lines)
	if value != "" && totalLines <= lines {
		// Bubbles v0.20 conserva un viewport interno con YOffset obsoleto cuando
		// el host auto-redimensiona el textarea por líneas soft-wrapped. Resetear
		// el valor fuerza el viewport interno a volver arriba y mantiene visible
		// todo el texto que ahora cabe en la caja.
		m.textarea.SetValue(value)
		m.textarea.CursorEnd()
	}
	return true
}

// syncInputHeight hace crecer la caja de entrada según las líneas realmente
// visibles (incluyendo wrap por ancho), evitando que la caja se dibuje
// encima del transcript cuando el texto envuelve.
func (m *ChatModel) syncInputHeight() {
	if !m.setInputHeightForContent() {
		return
	}
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
}

func (m *ChatModel) paletteView(w int) string {
	if !m.paletteOpen {
		return ""
	}
	return SuggestionMenu{
		Items:    m.paletteRows,
		Selected: m.paletteIdx,
		Width:    w - 2,
		Theme:    m.ctx.Styles.Theme,
		Query:    m.textarea.Value(),
	}.View()
}

func (m *ChatModel) inputBoxView(w int) string {
	s := m.ctx.Styles
	boxWidth := w - 2
	if boxWidth < 1 {
		boxWidth = 1
	}
	box := s.InputBoxFocused.Width(boxWidth).Render(m.textarea.View())
	if m.mode == ModeBash {
		box = s.Badge.Render(" BASH ") + "\n" + box
	}
	return box
}

// queuePanelView renderiza un panel FIJO justo encima de la caja de entrada
// con los mensajes que el usuario envió mientras Lilith ya estaba trabajando.
// A diferencia de un mensaje de sistema, este panel no se desplaza con el
// scroll del transcript: siempre está visible mientras haya cola pendiente,
// y desaparece a medida que se drena.
func (m *ChatModel) queuePanelView(w int) string {
	if len(m.queue) == 0 {
		return ""
	}
	s := m.ctx.Styles
	boxWidth := w - 2
	if boxWidth < 10 {
		boxWidth = w
	}
	header := "📥 En cola · " + fmt.Sprintf("%d pendiente(s)", len(m.queue))
	if m.streaming {
		header += " · se ejecutarán al terminar el turno actual"
	}
	header += "  (Ctrl+C vacía la cola)"

	lines := []string{s.Accent.Render(header)}
	maxShow := 5
	for i, msg := range m.queue {
		if i >= maxShow {
			lines = append(lines, s.Muted.Render(fmt.Sprintf("  … y %d más", len(m.queue)-maxShow)))
			break
		}
		prefix := fmt.Sprintf("  %d. ", i+1)
		// Ancho útil para el contenido tras el prefijo, dejando margen para
		// el borde y padding del panel.
		avail := boxWidth - len(prefix) - 4
		if avail < 10 {
			avail = 10
		}
		lines = append(lines, s.Muted.Render(prefix)+truncateOneLine(firstLine(msg), avail))
	}
	body := strings.Join(lines, "\n")
	panel := lipgloss.NewStyle().
		Width(boxWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.Theme.Primary).
		Padding(0, 1).
		Render(body)
	return panel
}

// truncateOneLine recorta text a maxRunes visibles añadiendo "…" si sobra.
// Colapsa espacios y saltos para mantener el panel en una sola fila por item.
func truncateOneLine(text string, maxRunes int) string {
	text = strings.ReplaceAll(text, "\t", " ")
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 1 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes-1]) + "…"
}
// pinnedActivityView renderiza el shimmer de actividad FIJO justo encima de
// la caja de entrada. No se mueve con el scroll y no lo tapan las burbujas de
// tool, así el usuario siempre sabe si Lilith sigue trabajando o pensando.
func (m *ChatModel) pinnedActivityView(w int) string {
	if !(m.thinking || m.working) {
		return ""
	}
	var body string
	if m.working {
		body = RenderWorking(m.thinkingFrame)
	} else {
		body = RenderThinking(m.thinkingFrame)
	}
	boxWidth := w - 2
	if boxWidth < 10 {
		boxWidth = w
	}
	return lipgloss.NewStyle().Width(boxWidth).Padding(0, 1).Render(body)
}


func (m *ChatModel) bottomChromeHeight(w int) int {
	parts := []string{}
	if act := m.pinnedActivityView(w); act != "" {
		parts = append(parts, act)
	}
	if queue := m.queuePanelView(w); queue != "" {
		parts = append(parts, queue)
	}
	if palette := m.paletteView(w); palette != "" {
		parts = append(parts, palette)
	}
	parts = append(parts,
		m.inputBoxView(w),

		RenderStatusBar(m.ctx, string(m.mode), 0, 0),
	)
	height := 1 // separador entre transcript y controles inferiores
	for i, part := range parts {
		height += lipgloss.Height(part)
		if i < len(parts)-1 {
			height++
		}
	}
	return height
}

func (m *ChatModel) updatePalette() {
	// (ver syncInputHeight para el alto de la caja)
	val := m.textarea.Value()
	// Aceptamos "/", "/skills:", y cualquier fragmento sin espacios ni saltos.
	// Los ':' son válidos para permitir el autocompletado de /skills:<nombre>.
	if strings.HasPrefix(val, "/") && !strings.Contains(val, "\n") && !strings.Contains(val, " ") {
		m.paletteOpen = true
		m.paletteRows = m.buildPalette(val)
		if m.paletteIdx >= len(m.paletteRows) {
			m.paletteIdx = 0
		}
	} else {
		m.paletteOpen = false
		m.paletteIdx = 0
		m.paletteRows = nil
	}
}

// buildPalette mezcla los comandos slash con las skills (Claude Agent Skills)
// disponibles. Las skills aparecen como entradas virtuales /skills:<nombre>
// pero también se autocompletan por su nombre sin el prefijo, tal como pidió
// el usuario. Filtramos por subsecuencia igual que en FilterCommands.
func (m *ChatModel) buildPalette(query string) []SlashCommand {
	rows := FilterCommands(query)
	if !m.skillsEnabled() {
		return rows
	}
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	q = strings.TrimPrefix(q, "skills:")
	for _, s := range m.loadSkills() {
		if q != "" {
			if _, ok := subsequenceMatch(s.Name, q); !ok {
				if _, ok2 := subsequenceMatch("skills:"+s.Name, q); !ok2 {
					continue
				}
			}
		}
		name := "skills:" + s.Name
		desc := "skill · " + s.Description
		skillName := s.Name
		rows = append(rows, SlashCommand{
			Name:        name,
			Description: desc,
			Run: func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd {
				return chat.invokeSkill(skillName, args)
			},
		})
	}
	return rows
}

func (m *ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Resize(v.Width, v.Height)
		return m, nil

	case thinkingTickMsg:
		if !m.thinking && !m.working {
			return m, nil
		}
		m.thinkingFrame = v.frame
		m.refreshTranscript(true)
		return m, thinkingTick(v.frame)


	case cmdElapsedTickMsg:
		// Refrescamos el transcript para que la línea "Elapsed …" avance de
		// forma suave. Reprogramamos mientras siga habiendo comandos vivos
		// o el turno esté en streaming.
		if !m.hasRunningCommand() && !m.streaming {
			m.cmdTickActive = false
			return m, nil
		}
		m.refreshTranscript(true)
		return m, cmdElapsedTick()

	case chatStreamMsg:
		if v.err != nil {
			m.streaming = false
			m.thinking = false
			m.working = false
			m.AddError("Error del proveedor: " + v.err.Error())
			return m, nil
		}
		if len(v.superseded) > 0 {
			for _, idx := range v.superseded {
				if p := m.livePanels[idx]; p != nil && !p.Failed {
					p.MarkSuperseded()
				}
			}
			m.refreshTranscript(true)
		}
		if len(v.toolCalls) > 0 {
			m.applyToolCalls(v.toolCalls)
			// Una tool call empieza a ser trabajo desde el PRIMER snapshot,
			// incluso mientras el proveedor todavía está transmitiendo sus
			// argumentos. Antes se apagaba "Pensando" al crear un panel de
			// archivo/comando, pero "Trabajando" sólo se encendía al llegar
			// el chunk final. En llamadas grandes (write_file, apply_diff, etc.)
			// eso dejaba varios segundos con ambos flags en false y el indicador
			// desaparecía exactamente mientras el panel decía "running".
			m.thinking = false
			m.working = true
			if v.partial {
				m.refreshTranscript(true)
				if v.ch != nil {
					return m, streamPump(v.ch)
				}
				return m, nil
			}
			m.pendingCall = append(m.pendingCall, v.toolCalls...)
		}
		if v.done {
			text := m.streamBuf.String()
			last := m.lastAssistantIndex()
			if last >= 0 {
				m.messages[last].Content = text
			}
			m.streamBuf.Reset()

			if len(m.pendingCall) > 0 {
				calls := m.pendingCall
				m.pendingCall = nil
				m.history = append(m.history, openai.Message{
					Role: "assistant", Content: text, ToolCalls: calls,
				})
				if text == "" && last >= 0 {
					m.messages = append(m.messages[:last], m.messages[last+1:]...)
				}
				m.applyToolCalls(calls)
				for _, c := range calls {
					if IsFileTool(c.Function.Name) || IsCommandTool(c.Function.Name) {
						continue
					}
					m.messages = append(m.messages, ChatMessage{
						Kind: MsgTool, Content: describeCall(c), Time: time.Now(),
					})
				}
				// Marca el instante de arranque de los command panels para
				// que el reloj "Elapsed" arranque en cero al dispatch.
				for _, c := range calls {
					if cp := m.cmdByCall[c.ID]; cp != nil {
						cp.Start()
					}
				}
				m.thinking = false
				m.working = true
				m.refreshTranscript(true)
				batch := []tea.Cmd{m.runTools(calls), thinkingTick(m.thinkingFrame)}

				if tick := m.maybeStartElapsedTick(); tick != nil {
					batch = append(batch, tick)
				}
				return m, tea.Batch(batch...)
			}

			if text != "" {
				m.history = append(m.history, openai.Message{Role: "assistant", Content: text})
				m.toolFallback = ""
			} else if last >= 0 {
				// Tras una tool call, algunos modelos cierran el turno sin texto.
				// En ese caso mostramos el resultado de la herramienta como cierre
				// determinista en vez de una falsa respuesta vacía.
				if fallback := strings.TrimSpace(m.toolFallback); fallback != "" {
					m.messages[last].Content = fallback
					m.history = append(m.history, openai.Message{Role: "assistant", Content: fallback})
					m.toolFallback = ""
				} else {
					// El modelo no devolvió texto ni herramientas: no dejamos una
					// burbuja vacía que parezca que Lilith no responde.
					m.messages[last].Content = "(el modelo no devolvió contenido)"
				}
			}
			m.streaming = false
			m.thinking = false
			m.working = false
			m.refreshTranscript(true)
			m.persist()
			return m, m.drainQueue()
		}
		if v.delta != "" {
			m.thinking = false
			m.streamBuf.WriteString(v.delta)
			last := m.lastAssistantIndex()
			if last >= 0 {
				m.messages[last].Content = m.streamBuf.String()
			}
			m.refreshTranscript(true)
		}
		if v.thinking != "" {
			if m.thinkingActive == nil {
				m.thinkingActive = &ThinkingPanel{Expanded: true}
				panel := ChatMessage{Kind: MsgThinking, Thinking: m.thinkingActive, Time: time.Now()}
				last := m.lastAssistantIndex()
				if last >= 0 {
					m.messages = append(m.messages[:last], append([]ChatMessage{panel}, m.messages[last:]...)...)
				} else {
					m.messages = append(m.messages, panel)
				}
			}
			m.thinkingActive.Append(v.thinking)
			// El reasoning sigue siendo una fase activa de "Pensando". Mantener
			// el shimmer evita otro hueco visual entre el primer delta de
			// reasoning y la siguiente tool call/respuesta. Si ya estamos en una
			// tool call, "Trabajando" tiene prioridad.
			if !m.working {
				m.thinking = true
			}
			m.refreshTranscript(true)
		}
		if v.thinkingDone && m.thinkingActive != nil {
			m.thinkingActive.Finish()
			m.thinkingActive = nil
			m.refreshTranscript(true)
		}
		if v.ch != nil {
			return m, streamPump(v.ch)
		}
		return m, nil

	case toolResultsMsg:
		// No apagamos m.working aquí: el turno sigue activo (vamos a llamar de
		// nuevo al modelo con runTurn). Dejar working en true evita que el
		// shimmer parpadee o desaparezca entre la tool y la siguiente respuesta.

		if v.err != nil {
			m.streaming = false
			m.thinking = false
			m.working = false
			m.AddError(v.err.Error())
			return m, nil
		}
		m.history = append(m.history, v.results...)
		m.toolFallback = summarizeToolResults(v.results)

		for _, r := range v.results {
			if p := m.panelByCall[r.ToolCallID]; p != nil {
				p.Finish(r.Content)
				continue
			}
			if cp := m.cmdByCall[r.ToolCallID]; cp != nil {
				cp.Finish(r.Content)
				continue
			}
			m.messages = append(m.messages, ChatMessage{
				Kind: MsgTool, Content: "  ↳ " + firstLine(r.Content), Time: time.Now(),
			})
		}
		m.refreshTranscript(true)
		return m, m.runTurn()

	case bashResultMsg:
		m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: v.output, Time: time.Now()})
		m.refreshTranscript(true)
		return m, nil

	case tea.KeyMsg:
		key := v.String()
		// Pegado del portapapeles (bracketed paste): siempre va al textarea
		// como texto literal. Nunca debe interpretarse como Enter, comandos
		// ni atajos, aunque contenga saltos de línea o caracteres de control.
		if v.Paste {
			var cmd tea.Cmd
			prev := m.textarea.Value()
			m.textarea, cmd = m.textarea.Update(msg)
			if m.textarea.Value() != prev {
				m.updatePalette()
				m.syncInputHeight()
			}
			return m, cmd
		}
		// Teclas de scroll: siempre van al viewport, incluso durante streaming
		// o ejecución de herramientas. Permiten leer el historial mientras
		// Lilith trabaja.
		if isScrollKey(key) {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.userScrolled = !m.viewport.AtBottom()
			return m, cmd
		}
		if m.paletteOpen {
			switch key {
			case "up":
				if m.paletteIdx > 0 {
					m.paletteIdx--
				}
				return m, nil
			case "down":
				if m.paletteIdx < len(m.paletteRows)-1 {
					m.paletteIdx++
				}
				return m, nil
			case "tab":
				if len(m.paletteRows) > 0 {
					c := m.paletteRows[m.paletteIdx]
					m.textarea.SetValue("/" + c.Name)
					m.textarea.CursorEnd()
					m.updatePalette()
				}
				return m, nil
			case "esc":
				m.paletteOpen = false
				return m, nil
			case "enter":
				if len(m.paletteRows) > 0 {
					c := m.paletteRows[m.paletteIdx]
					m.textarea.Reset()
					m.paletteOpen = false
					return m, c.Run(m.ctx, m, "")
				}
			}
		}
		switch key {
		case "ctrl+o", "ctrl+j", "ctrl+k":
			all := m.panels()
			if len(all) == 0 {
				return m, nil
			}
			if !m.panelPinned || m.panelSel < 0 || m.panelSel >= len(all) {
				m.panelSel = len(all) - 1
			}
			switch key {
			case "ctrl+j":
				m.panelSel = (m.panelSel + 1) % len(all)
				m.panelPinned = true
			case "ctrl+k":
				m.panelSel = (m.panelSel - 1 + len(all)) % len(all)
				m.panelPinned = true
			default:
				all[m.panelSel].Expanded = !all[m.panelSel].Expanded
			}
			m.refreshTranscript(true)
			return m, nil
		case "ctrl+r":
			// Toggle del último panel de "pensamiento".
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Kind == MsgThinking && m.messages[i].Thinking != nil {
					m.messages[i].Thinking.Expanded = !m.messages[i].Thinking.Expanded
					m.refreshTranscript(true)
					break
				}
			}
			return m, nil
		case "ctrl+z":
			// Nunca suspender ni cerrar de golpe: es demasiado fácil pulsarlo
			// por error mientras Lilith trabaja.
			return m, nil
		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel()
				// Sanear: si el turno cortado dejó tool_calls sin ejecutar,
				// añade outputs sintéticos para no romper el siguiente
				// request con "No tool output found for function call ...".
				if len(m.pendingCall) > 0 {
					for _, c := range m.pendingCall {
						m.history = append(m.history, toolMessage(c, "error: cancelado por el usuario."))
					}
					m.pendingCall = nil
				}
				dropped := len(m.queue)
				m.queue = nil
				notice := "Tarea cancelada."
				if dropped > 0 {
					notice = fmt.Sprintf("Tarea cancelada. %d mensaje(s) en cola descartados.", dropped)
				}
				m.AddSystem(notice + " Pulsa Ctrl+C otra vez para salir, o /exit.")
				m.streaming = false
				m.thinking = false
				m.working = false

				m.quitPrimedAt = time.Now()
				if m.ctx.Width > 0 && m.ctx.Height > 0 {
					m.Resize(m.ctx.Width, m.ctx.Height)
				}
				return m, nil
			}
			// Sin tarea activa: si hay cola pendiente, Ctrl+C la limpia
			// primero (no cierra la app hasta un segundo Ctrl+C).
			if len(m.queue) > 0 {
				dropped := len(m.queue)
				m.queue = nil
				m.AddSystem(fmt.Sprintf("Cola vaciada (%d mensaje(s) descartados).", dropped))
				m.quitPrimedAt = time.Now()
				if m.ctx.Width > 0 && m.ctx.Height > 0 {
					m.Resize(m.ctx.Width, m.ctx.Height)
				}
				return m, nil
			}

			if !m.quitPrimedAt.IsZero() && time.Since(m.quitPrimedAt) < 2*time.Second {
				return m, tea.Quit
			}
			m.quitPrimedAt = time.Now()
			m.AddSystem("Pulsa Ctrl+C otra vez para salir, o usa /exit.")
			return m, nil
		case "shift+enter", "alt+enter", "ctrl+enter":
			// Nueva línea explícita dentro del textarea.
			m.textarea.InsertString("\n")
			m.lastKeyAt = time.Now()
			m.syncInputHeight()
			return m, nil
		case "enter":
			// Heurística anti-paste: si la tecla previa fue hace muy poco,
			// asumimos que este Enter forma parte de un pegado multi-línea
			// y lo insertamos como salto de línea en lugar de enviar.
			// Bubbletea entrega cada rune del paste como un KeyMsg
			// separado; en Windows/PowerShell la separación puede llegar
			// a las decenas de ms por render, así que usamos un umbral
			// amplio (200ms).
			if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 200*time.Millisecond {
				m.textarea.InsertString("\n")
				m.lastKeyAt = time.Now()
				m.syncInputHeight()
				return m, nil
			}

			val := strings.TrimSpace(m.textarea.Value())
			if val == "" {
				return m, nil
			}
			m.lastKeyAt = time.Time{}
			return m.submit(val)
		}
		// Cualquier otra KeyMsg cuenta como actividad reciente de teclado
		// para la heurística de paste (excepto el propio Enter, que se
		// resetea al enviar arriba).
		m.lastKeyAt = time.Now()
	}

	var cmds []tea.Cmd
	prev := m.textarea.Value()
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)
	if m.textarea.Value() != prev {
		m.updatePalette()
		m.syncInputHeight()
	}
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
		// El viewport puede haberse desplazado por rueda del mouse. Ajusta
		// el flag para que el auto-scroll respete la posición del usuario.
		if _, isMouse := msg.(tea.MouseMsg); isMouse {
			m.userScrolled = !m.viewport.AtBottom()
		}
	}
	return m, tea.Batch(cmds...)
}

// drainQueue arranca el siguiente mensaje encolado (si lo hay) una vez que
// termina el turno actual. Devuelve nil si la cola está vacía. Sólo se
// procesa un mensaje por vez: la propia lógica de submit vuelve a llamar a
// drainQueue al terminar cada turno.
func (m *ChatModel) drainQueue() tea.Cmd {
	if m.streaming || len(m.queue) == 0 {
		return nil
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	// Al drenar, el panel fijo se re-renderiza automáticamente y la caja
	// vuelve a recalcular altura porque el chrome inferior cambió.
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	_, cmd := m.submit(next)
	return cmd
}

func (m *ChatModel) submit(val string) (tea.Model, tea.Cmd) {
	m.textarea.Reset()
	m.paletteOpen = false
	m.syncInputHeight()

	// Sólo una tarea a la vez: si Lilith ya está trabajando, encolamos el
	// mensaje del usuario y lo drenamos al terminar. El aviso NO va al
	// transcript (subiría con el scroll y se perdería): en su lugar
	// aparece en un panel fijo justo encima de la caja de entrada.
	if m.streaming {
		m.queue = append(m.queue, val)
		if m.ctx.Width > 0 && m.ctx.Height > 0 {
			m.Resize(m.ctx.Width, m.ctx.Height)
		}
		return m, nil
	}


	if strings.HasPrefix(val, "/") {
		firstSpace := strings.IndexByte(val, ' ')
		name := val
		args := ""
		if firstSpace > 0 {
			name = val[:firstSpace]
			args = strings.TrimSpace(val[firstSpace+1:])
		}
		// /skills:<nombre> [args] inyecta la SKILL.md correspondiente antes
		// del prompt del usuario y arranca el turno. También aceptamos
		// /skill:<nombre> por comodidad.
		lname := strings.ToLower(strings.TrimPrefix(name, "/"))
		if strings.HasPrefix(lname, "skills:") || strings.HasPrefix(lname, "skill:") {
			skillName := strings.TrimPrefix(lname, "skills:")
			skillName = strings.TrimPrefix(skillName, "skill:")
			return m, m.invokeSkill(skillName, args)
		}
		if cmd := FindCommand(name); cmd != nil {
			return m, cmd.Run(m.ctx, m, args)
		}
		m.AddError("Comando no reconocido: " + name)
		return m, nil
	}
	if strings.HasPrefix(val, "!") {
		cmd := strings.TrimSpace(val[1:])
		if cmd == "" {
			return m, nil
		}
		m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: "$ " + cmd, Time: time.Now()})
		m.refreshTranscript(true)
		return m, m.runBash(cmd)
	}

	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: val, Time: time.Now()})
	m.history = append(m.history, openai.Message{Role: "user", Content: val})
	// Selección perezosa: sólo los esquemas que este turno puede necesitar.
	m.activeTools = tools.Select(val)
	m.toolSteps = 0
	m.toolFallback = ""
	m.persist()
	m.refreshTranscript(true)
	return m, m.runTurn()
}

// runTurn envía el historial actual al modelo con los esquemas de herramientas
// activos y arranca el streaming.
func (m *ChatModel) runTurn() tea.Cmd {
	active := m.ctx.Providers.Active()
	provider := m.ctx.Providers.FindProvider(active.ProviderID)
	if provider == nil {
		m.AddError("No hay un proveedor activo. Usa /login o /providers.")
		return nil
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: "", Time: time.Now()})
	m.livePanels = nil
	m.panelByCall = nil
	m.cmdPanels = nil
	m.cmdByCall = nil
	m.thinkingActive = nil
	m.streaming = true
	m.thinking = true
	m.working = false

	m.thinkingFrame = 0
	m.streamBuf.Reset()
	m.refreshTranscript(true)

	msgs := make([]openai.Message, 0, len(m.history)+1)
	msgs = append(msgs, openai.Message{Role: "system", Content: systemPrompt(len(m.activeTools) > 0, m.skillsBlock())})
	msgs = append(msgs, m.history...)

	var schemas []any
	for _, s := range tools.Schemas(m.activeTools) {
		schemas = append(schemas, s)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	m.cancel = cancel
	req := openai.Request{
		Provider: *provider,
		Model:    active.ModelID,
		Messages: msgs,
		Stream:   true,
		Tools:    schemas,
	}
	ch := m.ctx.Client.Stream(ctx, req)
	batch := []tea.Cmd{streamPump(ch), thinkingTick(0)}
	if tick := m.maybeStartElapsedTick(); tick != nil {
		batch = append(batch, tick)
	}
	// Al enviar un turno nuevo el usuario espera ver la respuesta: soltamos
	// el "pin" para que vuelva al fondo automáticamente.
	m.userScrolled = false
	return tea.Batch(batch...)
}

// contextUsage devuelve los tokens estimados del turno actual y la ventana
// máxima del modelo activo (resuelta contra el catálogo de modelos).
func (m *ChatModel) contextUsage() (int, int) {
	active := m.ctx.Providers.Active()
	if active.ModelID == "" {
		return 0, 0
	}
	msgs := make([]openai.Message, 0, len(m.history)+1)
	msgs = append(msgs, openai.Message{Role: "system", Content: systemPrompt(len(m.activeTools) > 0, m.skillsBlock())})
	msgs = append(msgs, m.history...)
	used := EstimateTokens(msgs)
	maxCtx := m.ctx.Providers.FindProvider(active.ProviderID).ContextWindow(active.ModelID)
	return used, maxCtx
}

// maxToolSteps evita bucles infinitos de herramientas en un mismo turno.
// Ediciones grandes (str_replace + read_file encadenados) pasan de 25
// fácilmente; 60 es un techo cómodo sin ser un bucle infinito real.
const maxToolSteps = 60

// runTools ejecuta el lote de llamadas y devuelve los mensajes `tool`.
func (m *ChatModel) runTools(calls []openai.ToolCall) tea.Cmd {
	m.toolSteps++
	if m.toolSteps > maxToolSteps {
		// Sanear el historial: el assistant previo dejó tool_calls sin
		// ejecutar; inyectamos outputs sintéticos para que la próxima
		// petición no reviente con "No tool output found for function
		// call ..." en la Responses API.
		stubs := make([]openai.Message, 0, len(calls))
		for _, c := range calls {
			stubs = append(stubs, toolMessage(c, "error: turno interrumpido (límite de pasos de herramientas alcanzado)."))
		}
		m.history = append(m.history, stubs...)
		return func() tea.Msg {
			return toolResultsMsg{err: errors.New("límite de pasos de herramientas alcanzado en este turno (aumentado a " + fmt.Sprint(maxToolSteps) + "). Pídeme continuar si aún falta.")}
		}
	}

	root, _ := os.Getwd()
	materialize := func(names []string) {
		set := map[string]bool{}
		for _, n := range m.activeTools {
			set[n] = true
		}
		for _, n := range names {
			if !set[n] {
				set[n] = true
				m.activeTools = append(m.activeTools, n)
			}
		}
	}
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	env := tools.Env{Root: root, Materialize: materialize, Seen: m.seen}
	return func() tea.Msg {
		results := make([]openai.Message, 0, len(calls))
		for _, c := range calls {
			args := map[string]any{}
			if strings.TrimSpace(c.Function.Arguments) != "" {
				if err := json.Unmarshal([]byte(c.Function.Arguments), &args); err != nil {
					results = append(results, toolMessage(c, "error: argumentos JSON inválidos: "+err.Error()))
					continue
				}
			}
			out, err := tools.Execute(context.Background(), c.Function.Name, args, env)
			if err != nil {
				out = "error: " + err.Error()
			}
			results = append(results, toolMessage(c, out))
		}
		return toolResultsMsg{results: results}
	}
}

func toolMessage(c openai.ToolCall, content string) openai.Message {
	return openai.Message{
		Role:       "tool",
		ToolCallID: c.ID,
		Name:       c.Function.Name,
		Content:    content,
	}
}

// runBash ejecuta el modo `!comando` directamente, sin pasar por el modelo.
func (m *ChatModel) runBash(command string) tea.Cmd {
	root, _ := os.Getwd()
	return func() tea.Msg {
		out, err := tools.Execute(context.Background(), "run_terminal_command",
			map[string]any{"command": command}, tools.Env{Root: root})
		if err != nil {
			out = "error: " + err.Error()
		}
		return bashResultMsg{output: out}
	}
}

type bashResultMsg struct{ output string }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	if s == "" {
		s = "(no output)"
	}
	return s
}

func summarizeToolResults(results []openai.Message) string {
	if len(results) == 0 {
		return ""
	}
	var lines []string
	for _, r := range results {
		content := strings.TrimSpace(r.Content)
		if content == "" {
			continue
		}
		name := strings.TrimSpace(r.Name)
		if name == "write_file" || name == "str_replace" {
			lines = append(lines, content)
			continue
		}
		if strings.HasPrefix(content, "error:") {
			lines = append(lines, content)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		return lines[0]
	}
	return strings.Join(lines, "\n")
}

// describeCall renders a tool invocation as a shell-like command line. We
// keep it terse and drop the JSON blob so the transcript reads like a real
// terminal ($ tool arg1=… arg2=…) instead of a raw wire dump.
func describeCall(c openai.ToolCall) string {
	args := prettyToolArgs(c.Function.Arguments)
	if args == "" {
		return "$ " + c.Function.Name
	}
	return "$ " + c.Function.Name + " " + args
}

// prettyToolArgs turns a JSON args blob into a compact key=value summary.
// Long string values are truncated so the line fits one row.
func prettyToolArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		flat := strings.ReplaceAll(raw, "\n", " ")
		if len(flat) > 80 {
			flat = flat[:80] + "…"
		}
		return flat
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable order: path/command/url first, then alphabetical.
	priority := map[string]int{"path": 0, "paths": 0, "command": 0, "url": 0, "pattern": 1, "query": 1}
	sortKeys := func(a, b string) bool {
		pa, pb := 5, 5
		if v, ok := priority[a]; ok {
			pa = v
		}
		if v, ok := priority[b]; ok {
			pb = v
		}
		if pa != pb {
			return pa < pb
		}
		return a < b
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if sortKeys(keys[j], keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := fmt.Sprintf("%v", m[k])
		v = strings.ReplaceAll(v, "\n", " ")
		if len(v) > 40 {
			v = v[:40] + "…"
		}
		parts = append(parts, k+"="+v)
	}
	line := strings.Join(parts, " ")
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	return line
}


// skillsEnabled devuelve el toggle persistido en settings.json (recargado
// cada vez: /config puede haberlo cambiado sin que el chat lo sepa).
func (m *ChatModel) skillsEnabled() bool {
	s, _ := config.Load(m.ctx.ConfigDir)
	return s.SkillsEnabled
}

// loadSkills descubre las skills user + project. Barato (dos ReadDir).
func (m *ChatModel) loadSkills() []skills.Skill {
	return skills.Load(skills.LoadOptions{
		UserDir:    skills.UserDir(m.ctx.ConfigDir),
		ProjectDir: skills.ProjectDir(m.project),
	})
}

// skillsBlock renderiza el bloque XML de skills disponibles cuando el toggle
// está activo. Devuelve "" si está desactivado o si no hay skills.
func (m *ChatModel) skillsBlock() string {
	if !m.skillsEnabled() {
		return ""
	}
	return skills.FormatForPrompt(m.loadSkills())
}

// invokeSkill maneja "/skills:<nombre> [args]": lee SKILL.md, la inyecta como
// mensaje de usuario con instrucciones explícitas y arranca el turno. Si la
// skill no existe o los skills están desactivados, avisa por el chat.
func (m *ChatModel) invokeSkill(name, args string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		m.AddError("Uso: /skills:<nombre> [instrucciones extra]")
		return nil
	}
	if !m.skillsEnabled() {
		m.AddError("Las skills están desactivadas. Actívalas en /config.")
		return nil
	}
	list := m.loadSkills()
	sk := skills.Find(list, name)
	if sk == nil {
		m.AddError("Skill no encontrada: " + name + ". Revisa ~/.li/skills o ./.li/skills.")
		return nil
	}
	body, err := skills.ReadContent(*sk)
	if err != nil {
		m.AddError("No se pudo leer la skill " + name + ": " + err.Error())
		return nil
	}
	var b strings.Builder
	b.WriteString("The user explicitly invoked skill `")
	b.WriteString(sk.Name)
	b.WriteString("` (from ")
	b.WriteString(sk.FilePath)
	b.WriteString("). Follow the instructions below exactly, then perform the requested task.\n\n")
	b.WriteString("--- SKILL: ")
	b.WriteString(sk.Name)
	b.WriteString(" ---\n")
	b.WriteString(body)
	b.WriteString("\n--- END SKILL ---\n")
	if strings.TrimSpace(args) != "" {
		b.WriteString("\nUser arguments: ")
		b.WriteString(args)
		b.WriteString("\n")
	}
	payload := b.String()

	visible := "/skills:" + sk.Name
	if strings.TrimSpace(args) != "" {
		visible += " " + args
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: visible, Time: time.Now()})
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: "Skill cargada: " + sk.Name + " (" + sk.Source + ")", Time: time.Now()})
	m.history = append(m.history, openai.Message{Role: "user", Content: payload})
	m.activeTools = tools.Select(body + "\n" + args)
	if len(m.activeTools) == 0 {
		// Al menos permite leer/escribir/ejecutar cuando la skill lo pide.
		m.activeTools = []string{"tool_search", "read_files", "write_file", "str_replace", "run_terminal_command"}
	}
	m.toolSteps = 0
	m.toolFallback = ""
	m.persist()
	m.refreshTranscript(true)
	return m.runTurn()
}


// Kept in English on purpose: LLMs follow English tool-use guidance more
// reliably than translated instructions, and this string travels on every
// request so it stays tight. Rules are distilled from pi.dev's
// coding-agent/system-prompt and hardened for our tool set.
func systemPrompt(withTools bool, skillsBlock string) string {
	base := "You are Lilith, an expert coding assistant that operates inside " +
		"the user's terminal. You help by reading files, running commands, and " +
		"editing / creating source files with real tool calls. " +
		"Reply to the user in the same language they use (default Spanish for " +
		"conversational replies), but keep tool arguments, file paths, code, " +
		"identifiers, and shell commands in their original language. " +
		"Be concise, direct, and skip filler."
	if !withTools {
		if skillsBlock != "" {
			return base + skillsBlock
		}
		return base
	}
	cwd, _ := os.Getwd()
	return base + "\n\n" +
		"# Available tools (always prefer tools over pasting code in chat)\n" +
		"- read_files: read one or more real project files before touching them. Supports `offset` (1-indexed start line) and `limit` (max lines) to paginate large files without blowing the context.\n" +
		"- write_file: create a NEW file with the FULL final content. Never use it to edit an existing file unless the user explicitly asks for a full rewrite.\n" +
		"- str_replace: surgical edit(s) of an EXISTING file. Pass `path` plus either `old`/`new` OR an `edits: [{old,new}, ...]` array to apply several non-overlapping replacements in one call. Every `old` MUST be non-empty, copied byte-for-byte from the current file (whitespace, indentation and newlines included), and MUST appear EXACTLY ONCE — add 2-3 surrounding lines as anchor context if the snippet is not unique. `new` may be empty to delete the matched region. To INSERT new code, set `old` to an existing anchor line and repeat that anchor inside `new` together with the new lines. If an exact match fails, the tool retries with a fuzzy pass (Unicode quotes/dashes/spaces + trailing whitespace) — but do not rely on that as a substitute for reading the file first. Never call str_replace with an empty `old`.\n" +
		"- apply_diff: apply a unified diff (`@@ -a,b +c,d @@` hunks) to an existing file. Prefer this over str_replace when you already have a diff or when the change spans many hunks; context lines must match byte-for-byte.\n" +
		"- list_directory / glob / code_search: explore the repo. `code_search` accepts `glob`, `path`, `literal`, `ignore_case`, `context` (lines around each match) and `limit`.\n" +
		"- run_terminal_command: execute shell commands (build, tests, git, rg, ls). Output is tail-truncated; when truncated the note tells you the temp file path with the full stream so you can read_files it if needed.\n" +
		"- read_url: fetch documentation from a public URL.\n" +
		"- tool_search: enable extra tools on demand.\n\n" +

		"# Hard rules (MUST follow)\n" +
		"1. Never emit partial files, scaffolds, or placeholders such as " +
		"`// rest of the code`, `// TODO: fill in`, `...`, `<!-- rest -->`, " +
		"`# ... (omitted)`, `/* keep existing */`, or any equivalent. Every file " +
		"you write must compile / render as-is, ready to run.\n" +
		"2. If the user asks for MULTIPLE artifacts in one turn (e.g. the same " +
		"page in two languages, EN + ES; or client + server; or several tests), " +
		"produce ALL of them fully in the SAME turn, each with its complete " +
		"content. Never say \"same as above\" or expect the user to copy things.\n" +
		"3. To modify an existing file: first read it with read_files, then use " +
		"str_replace with a minimal, UNIQUE `old` window copied verbatim from the " +
		"file (never leave `old` empty). Do NOT rewrite the whole file with " +
		"write_file unless the user explicitly asks for a rewrite.\n" +
		"4. Preserve the project's conventions, style, imports and file layout. " +
		"Make the smallest change that satisfies the request.\n" +
		"5. Before finishing a task that touches code, run the project's build " +
		"and/or tests when they exist. Fix any regressions in the same turn.\n" +
		"6. Keep working across tool steps until the task is truly done. Do not " +
		"stop with \"do you want me to continue?\" unless you are actually blocked " +
		"(missing credentials, ambiguous requirement, destructive action).\n" +
		"7. Ask a clarifying question ONLY when the request is genuinely ambiguous. " +
		"For obvious tasks, just do the work.\n" +
		"8. When you use run_terminal_command, prefer non-interactive flags and a " +
		"reasonable timeout. Set `timeout_seconds` explicitly per call (default 30). " +
		"Use larger values for installs, builds, or test suites (e.g. 120, 300) and " +
		"short ones for cheap checks. When the command times out, all descendants are " +
		"killed automatically. Never run destructive commands (rm -rf, force pushes, " +
		"resetting git history) without an explicit user request.\n" +
		"9. When done, summarise in 1-3 lines: which files changed and what still " +
		"needs the user's attention (e.g. env vars, migrations).\n\n" +
		"Working directory: " + cwd + skillsBlock
}


// streamPump reads one chunk and forwards it as a chatStreamMsg, keeping the
// channel handle so the next tick can continue pumping.
func streamPump(ch <-chan openai.Chunk) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return chatStreamMsg{done: true}
		}
		if c.Err != nil {
			return chatStreamMsg{err: c.Err}
		}
		if c.Done {
			return chatStreamMsg{done: true}
		}
		return chatStreamMsg{ch: ch, delta: c.Delta, toolCalls: c.ToolCalls, partial: c.Partial, superseded: c.SupersededIndices, thinking: c.Thinking, thinkingDone: c.ThinkingDone}
	}
}

func (m *ChatModel) View() string {
	w := m.ctx.Width
	if w <= 0 {
		w = 80
	}

	// Viewport (transcript)
	transcript := m.viewport.View()
	// Barra de scroll a la derecha. Muestra la posición del viewport dentro
	// del transcript total (perilla deslizable).
	if bar := m.renderScrollbar(); bar != "" {
		transcript = lipgloss.JoinHorizontal(lipgloss.Top, transcript, bar)
	}

	used, maxCtx := m.contextUsage()
	parts := []string{transcript}
	if act := m.pinnedActivityView(w); act != "" {
		parts = append(parts, act)
	}
	if queue := m.queuePanelView(w); queue != "" {
		parts = append(parts, queue)
	}
	if palette := m.paletteView(w); palette != "" {
		parts = append(parts, palette)
	}

	parts = append(parts,
		m.inputBoxView(w),
		RenderStatusBar(m.ctx, string(m.mode), used, maxCtx),
	)
	return strings.Join(parts, "\n")
}

var _ tea.Model = (*ChatModel)(nil)
