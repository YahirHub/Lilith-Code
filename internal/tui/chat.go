package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/lilith/li/internal/tui/uikit/textarea"
	"github.com/lilith/li/internal/tui/uikit/viewport"

	"github.com/lilith/li/internal/agents"
	"github.com/lilith/li/internal/codeintel"
	compactctx "github.com/lilith/li/internal/compaction"
	"github.com/lilith/li/internal/config"
	ligoal "github.com/lilith/li/internal/goal"
	"github.com/lilith/li/internal/hooks"
	"github.com/lilith/li/internal/mcp"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/rewind"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/skills"
	"github.com/lilith/li/internal/subagents"
	litodo "github.com/lilith/li/internal/todo"
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
	MsgAgent
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
	// con toggle expandir/plegar y altura adaptativa hasta un máximo.
	Thinking *ThinkingPanel
	// Agent se usa en MsgAgent: progreso observable de un subagente aislado.
	// Vive dentro del transcript normal, nunca anclado al footer.
	Agent *AgentPanel
}

// InputMode is the state of the input bar (default | bash).
type InputMode string

const (
	ModeDefault InputMode = "default"
	ModeBash    InputMode = "bash"
)

type queueMode string

const (
	queueSteer    queueMode = "steer"
	queueFollowUp queueMode = "follow-up"
)

type queuedMessage struct {
	Text string
	Mode queueMode
}

// ChatModel is the main chat screen. Used via pointer.
type ChatModel struct {
	ctx           *AppContext
	viewport      viewport.Model
	textarea      textarea.Model
	messages      []ChatMessage
	mode          InputMode
	streaming     bool
	streamBuf     strings.Builder
	cancel        context.CancelFunc
	requestCancel context.CancelFunc
	// sessionCtx outlives individual turns so background subagents can keep
	// running while the parent is idle. It is canceled when Lilith exits, the
	// conversation is cleared or another persisted session becomes active.
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	// agentGeneration isolates event streams across /clear and session resume.
	// Background workers from an older session may finish after cancellation;
	// their captured generation prevents stale events entering the new chat.
	agentGeneration atomic.Uint64
	agentEventCh    chan agentEventEnvelope
	mcpRuntime      *mcp.Runtime
	mcpSignature    string
	mcpLoading      bool
	// Completed detached workers are delivered to the model at the next safe
	// request boundary, matching Claude's later-turn completion notification.
	pendingBackgroundAgentMessages []openai.Message

	// Cada request HTTP/SSE dentro de un mismo turno recibe un ID propio. Esto
	// es distinto de activeTurnID: un turno puede hacer varias peticiones al
	// proveedor entre tool calls. Si una petición se cancela de forma local
	// (por ejemplo por preflight FILE_EXISTS), sus chunks tardíos no deben
	// confundirse con la siguiente petición del mismo turno.
	requestSeq          uint64
	activeRequestID     uint64
	requestMessageStart int
	networkNoticeIndex  int

	// turnCtx abarca TODO el turno del usuario: streaming del proveedor y
	// herramientas. Escape cancela este contexto una sola vez y los resultados
	// tardíos quedan invalidados por activeTurnID.
	turnCtx             context.Context
	turnSeq             uint64
	activeTurnID        uint64
	turnProvider        string
	turnModel           string
	turnAgentMode       planstate.Mode
	turnPlanHandoff     string
	turnReasoningEffort string
	turnDeniedTools     map[string]bool
	turnSkillHooks      *hooks.Runner
	// turnGoalManaged snapshots whether this parent turn belongs to an active
	// durable goal. A completed/paused goal must never terminate an unrelated
	// later user request at its first tool boundary.
	turnGoalManaged bool
	runningCalls    []openai.ToolCall

	// history is the real conversation sent to the model (incluye mensajes
	// de herramienta), separada del transcript que se dibuja en pantalla.
	history     []openai.Message
	activeTools []string
	// Los schemas que ya se materializaron se conservan por modo durante la
	// sesión. Pi recomienda crecimiento aditivo para no romper el prefijo que
	// los proveedores pueden reutilizar mediante prompt caching. Antes Lilith
	// reconstruía/reemplazaba el set en cada prompt, invalidando esa caché aun
	// cuando el historial anterior era idéntico.
	buildToolCache []string
	planToolCache  []string
	pendingCall    []openai.ToolCall
	toolFallback   string

	// Persistencia de la conversación (historial por proyecto).
	store     *session.Store
	sess      *session.Session
	project   string
	codeIntel *codeintel.Manager

	// /rewind stores a cheap conversation checkpoint at each user boundary and
	// captures the workspace lazily before the first mutating tool. The mutable
	// turn state is behind a pointer because asynchronous tools and the persistent
	// chat screen must share one mutable checkpoint state. ChatModel itself is
	// always constructed and retained by pointer because it contains atomics.
	rewindStore *rewind.Store
	rewindTurn  *rewindTurnState

	// Agent definitions are session-scoped like Claude Code: discover once at
	// startup so repeated turns do not rescan multiple compatibility trees.
	// Restarting Lilith reloads files added/changed directly on disk.
	agentCatalog []agents.Agent

	// todos is the model-owned authoritative task plan for this session. It is
	// concurrency-safe because tools run outside the TUI state goroutine.
	todos *litodo.Manager

	// todoExpanded only controls presentation. The authoritative plan stays in
	// todo.Manager. Compact is the default so TodoWrite remains cheap on short
	// terminals; Ctrl+T or clicking the visible Todo block toggles the full list.
	todoExpanded bool

	// plans owns the selected primary agent mode (Build/Plan/Goal) and any approved
	// plan/questions for this session. The selected mode applies to the NEXT
	// turn; turnAgentMode snapshots it when a request starts, like OpenCode.
	plans *planstate.Manager

	// goals owns the Codex-style durable objective for this session. The goal is
	// persisted independently from chat text and can keep the agent running across
	// multiple autonomous continuation turns until complete, blocked or limited.
	goals             *ligoal.Manager
	goalRequestTokens int64

	// planQuestion is only presentation state. The authoritative questions and
	// partial answers live in plan.Manager so closing the dock or resuming a
	// session never loses decisions.
	planQuestion planQuestionDock

	// Paneles de archivo en vivo (creación/edición con diff plegable).
	livePanels  map[int]*FilePanel
	panelByCall map[string]*FilePanel
	panelSel    int
	// panelPinned indica que el usuario eligió un panel con ctrl+j/k. Mientras
	// sea falso, ctrl+o actúa siempre sobre la última ventana abierta.
	panelPinned bool
	// Paneles de comando en vivo (run_terminal_command con streaming).
	cmdPanels map[int]*CommandPanel
	cmdByCall map[string]*CommandPanel
	// agentPanels apunta al panel vivo más reciente de cada task_id. Una
	// reanudación crea un panel nuevo y reemplaza esta entrada sin modificar el
	// panel histórico anterior que ya quedó en el transcript.
	agentPanels map[string]*AgentPanel

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

	paletteOpen bool
	paletteIdx  int
	paletteRows []SlashCommand

	// userScrolled es true cuando el usuario desplazó el transcript hacia
	// arriba manualmente. Mientras lo esté, el auto-scroll a fondo queda
	// desactivado para poder leer historial aunque Lilith siga trabajando.
	userScrolled bool
	// cmdTickActive evita programar varios timers de "Elapsed" en paralelo.
	cmdTickActive bool

	// queue guarda los mensajes enviados mientras Lilith ya trabaja. Igual que
	// pi.dev, Enter crea una instrucción de steering (se entrega en la siguiente
	// frontera segura del agente) y Alt+Enter crea un follow-up (se ejecuta al
	// terminar el trabajo actual). Esc aborta y devuelve la cola al editor;
	// Alt+Up permite recuperarla sin cancelar la tarea.
	queue []queuedMessage

	// Compatibilidad para terminales/hosts que degradan un pegado multilínea
	// a teclas normales en vez de conservar bracketed paste. Un Enter normal
	// queda pendiente durante una ventana minúscula: si llega texto enseguida,
	// ese Enter era un salto del pegado; si no llega nada, se envía el mensaje.
	// Esto evita la heurística antigua "tecla previa + Enter rápido", que
	// rompía a quien escribe y pulsa Enter con rapidez.
	pasteFallbackActive bool
	pasteFallbackSeq    uint64
	pasteAwaitingLF     bool
	pendingEnter        bool
	pendingEnterSeq     uint64

	// assistantActive apunta únicamente a la respuesta textual del turno de
	// modelo ACTUAL. Se crea de forma perezosa al llegar el primer delta para
	// no mostrar una cabecera `lilith` vacía mientras la API aún no responde.
	assistantActive int
	// reasoningBuf conserva el razonamiento explícitamente expuesto por el
	// proveedor durante este paso del modelo. Además de mostrarse en vivo, se
	// adjunta al assistant que contiene tool_calls cuando el endpoint lo exige.
	reasoningBuf strings.Builder

	// Caché del prefijo estable del transcript. Durante un turno sólo cambia
	// la cola a partir del último mensaje del usuario; mantener renderizado el
	// historial anterior evita volver a pasar cientos de mensajes por el render Markdown
	// en cada delta SSE.
	transcriptPrefixLines []string
	transcriptPrefixCount int
	transcriptPrefixWidth int
	transcriptPrefixValid bool

	// Los deltas de streaming pueden llegar mucho más rápido de lo que una
	// terminal puede pintar. Agrupamos refrescos del viewport para no ejecutar
	// reconstruir el transcript por cada token, sin perder ningún texto acumulado.
	lastTranscriptRefresh       time.Time
	transcriptRefreshPending    bool
	transcriptRefreshAutoBottom bool

	// La barra de contexto se dibuja en cada View(). Antes su cálculo recorría
	// todo el historial y releía configuración/skills en cada frame.
	contextUsedCache     int
	contextMaxCache      int
	contextCacheDirty    bool
	contextCacheProvider string
	contextCacheModel    string
	contextCacheToolSig  string

	// Compaction runs as a one-off provider request outside the normal tool
	// stream. Auto compaction resumes the same active turn; manual /compact runs
	// while idle. IDs make late results harmless after Escape/cancellation.
	compacting                   bool
	compactionSeq                uint64
	activeCompactionID           uint64
	compactionCancel             context.CancelFunc
	autoCompactionSkipHistoryLen int

	// Persistencia incremental del turno activo. El historial API se guarda
	// completo en fronteras semánticas; entre ellas sólo escribimos un sidecar
	// con la cola mutable del transcript para no reserializar conversaciones
	// largas por cada token.
	liveBaseMessageCount int
	liveBaseHistoryCount int
	persistRevision      uint64
	livePersistPending   bool
	livePersistDirty     bool
	livePersistTimer     bool
	lastLivePersist      time.Time
}

// chatStreamMsg is emitted by the streaming pump for each SSE chunk.
type chatStreamMsg struct {
	turnID       uint64
	requestID    uint64
	ch           <-chan openai.Chunk
	delta        string
	toolCalls    []openai.ToolCall
	superseded   []int
	partial      bool
	thinking     string
	thinkingDone bool
	retry        *openai.RetryStatus
	done         bool
	err          error
}

// toolResultsMsg carries the outcome of a batch of tool calls.
type toolResultsMsg struct {
	turnID         uint64
	results        []openai.Message
	materialized   []string
	compactCallIDs []string
	recoverEditors bool
	recoverCreate  bool
	todoChanged    bool
	planQuestion   bool
	planCompleted  bool
	err            error
}

// manualAgentResultMsg is the result of an explicit @agent invocation. It uses
// the same turn cancellation root as normal chat work, but bypasses the parent
// LLM because the user addressed the child directly.
type manualAgentResultMsg struct {
	turnID uint64
	agent  string
	taskID string
	text   string
	err    error
}

// agentEventBatchMsg streams a short coalesced slice of child-agent progress
// into the TUI state loop. Token/reasoning deltas can be very frequent; batching them
// keeps parallel workers observable without forcing a full transcript render
// for every provider chunk.
type agentEventEnvelope struct {
	generation uint64
	event      subagents.Event
}

type agentEventBatchMsg struct {
	events []agentEventEnvelope
	ch     <-chan agentEventEnvelope
	done   bool
}

type agentEventStreamDoneMsg struct{}

// cmdElapsedTickMsg refresca el transcript a intervalo fijo mientras haya
// paneles de comando en ejecución, para que la línea "Elapsed …" avance de
// forma suave (antes dependía de que llegara delta streaming y saltaba a
// tirones).
type cmdElapsedTickMsg struct{}

// transcriptRefreshTickMsg aplica un refresco de viewport agrupado por
// refreshTranscriptStreaming.
type transcriptRefreshTickMsg struct{}

// livePersistDoneMsg confirma la escritura asíncrona del checkpoint mutable.
type livePersistDoneMsg struct {
	revision uint64
	err      error
}

// livePersistTickMsg despierta la escritura agrupada cuando llegaron muchos
// deltas dentro de la misma ventana de persistencia.
type livePersistTickMsg struct{}

// pasteEnterDecisionMsg resuelve un Enter ambiguo que llegó sin marcador de
// paste. Si durante la ventana no aparece más contenido, era un Enter humano y
// se ejecuta el submit. Un seq antiguo nunca puede enviar un valor más nuevo.
type pasteEnterDecisionMsg struct{ seq uint64 }

// pasteFallbackIdleMsg cierra la ráfaga confirmada de paste. Mientras está
// activa, los Enter posteriores son saltos de línea y nunca submits.
type pasteFallbackIdleMsg struct{ seq uint64 }

const (
	chatInputVisibleMaxHeight         = 8
	transcriptRefreshInterval         = 50 * time.Millisecond
	transcriptScrolledRefreshInterval = 250 * time.Millisecond
	livePersistInterval               = 200 * time.Millisecond

	// Sin bracketed paste no existe información suficiente para distinguir un
	// CR pegado de la tecla Enter. En vez de adivinar mirando la tecla ANTERIOR,
	// damos al input una ventana corta para ver si llega contenido DESPUÉS del
	// Enter. Los bytes de un paste llegan como una ráfaga; un Enter humano queda
	// solo y se envía al vencer esta ventana.
	pasteEnterDecisionWindow = 80 * time.Millisecond
	pasteFallbackIdleWindow  = 60 * time.Millisecond
)

func (m *ChatModel) resetPasteFallback() {
	m.pasteFallbackActive = false
	m.pasteAwaitingLF = false
	m.pendingEnter = false
	// Invalidar cualquier timer pendiente sin esperar a que dispare.
	m.pasteFallbackSeq++
	m.pendingEnterSeq++
}

func (m *ChatModel) armPasteFallback() uikit.Cmd {
	m.pasteFallbackActive = true
	m.pasteFallbackSeq++
	seq := m.pasteFallbackSeq
	return uikit.Tick(pasteFallbackIdleWindow, func(time.Time) uikit.Msg {
		return pasteFallbackIdleMsg{seq: seq}
	})
}

func (m *ChatModel) deferEnterSubmit() uikit.Cmd {
	m.pendingEnter = true
	m.pendingEnterSeq++
	seq := m.pendingEnterSeq
	return uikit.Tick(pasteEnterDecisionWindow, func(time.Time) uikit.Msg {
		return pasteEnterDecisionMsg{seq: seq}
	})
}

// confirmPendingEnterAsPaste convierte el Enter pendiente en texto sólo cuando
// existe evidencia posterior (otro fragmento del paste). Esa dirección de la
// comprobación es importante: escribir y pulsar Enter rápidamente no se
// confunde con un pegado, porque no hay una tecla posterior que lo confirme.
func (m *ChatModel) confirmPendingEnterAsPaste() uikit.Cmd {
	if !m.pendingEnter {
		return nil
	}
	m.pendingEnter = false
	m.pendingEnterSeq++
	m.textarea.InsertString("\n")
	m.updatePalette()
	m.syncInputHeight()
	return m.armPasteFallback()
}

func isPasteContinuationKey(v uikit.KeyMsg) bool {
	if v.Paste {
		return false
	}
	switch v.Type {
	case uikit.KeyRunes, uikit.KeySpace, uikit.KeyTab, uikit.KeyEnter, uikit.KeyCtrlJ:
		return true
	default:
		return false
	}
}

func cmdElapsedTick() uikit.Cmd {
	// CommandPanel muestra segundos enteros; refrescar dos veces por segundo
	// reconstruía el viewport sin aportar información visible adicional.
	return uikit.Tick(time.Second, func(time.Time) uikit.Msg { return cmdElapsedTickMsg{} })
}

func agentEventPump(ch <-chan agentEventEnvelope) uikit.Cmd {
	if ch == nil {
		return nil
	}
	return func() uikit.Msg {
		first, ok := <-ch
		if !ok {
			return agentEventStreamDoneMsg{}
		}
		events := make([]agentEventEnvelope, 0, 32)
		events = append(events, first)
		timer := time.NewTimer(35 * time.Millisecond)
		defer timer.Stop()
		for len(events) < 128 {
			select {
			case event, open := <-ch:
				if !open {
					return agentEventBatchMsg{events: events, done: true}
				}
				events = append(events, event)
			case <-timer.C:
				return agentEventBatchMsg{events: events, ch: ch}
			}
		}
		return agentEventBatchMsg{events: events, ch: ch}
	}
}

func (m *ChatModel) applyAgentEvent(event subagents.Event) {
	if m.agentPanels == nil {
		m.agentPanels = map[string]*AgentPanel{}
	}
	if event.Kind == subagents.EventStarted {
		panel := newAgentPanel(event)
		m.agentPanels[event.TaskID] = panel
		m.messages = append(m.messages, ChatMessage{Kind: MsgAgent, Agent: panel, Time: event.At})
		m.invalidateTranscriptCache()
		return
	}
	panel := m.agentPanels[event.TaskID]
	if panel == nil {
		// Defensive recovery for an event stream that starts after the task was
		// already created (for example a host attaching late to a resumed task).
		started := event
		started.Kind = subagents.EventStarted
		panel = newAgentPanel(started)
		m.agentPanels[event.TaskID] = panel
		m.messages = append(m.messages, ChatMessage{Kind: MsgAgent, Agent: panel, Time: event.At})
	}
	panel.Apply(event)
	if event.Background && subagents.IsTerminalEvent(event.Kind) {
		status := "completed"
		if event.Kind == subagents.EventFailed {
			status = "failed"
		} else if event.Kind == subagents.EventCanceled {
			status = "canceled"
		}
		m.queueBackgroundAgentCompletion(event.TaskID, event.AgentName, status, event.Content, event.At)
	}
	m.invalidateTranscriptCache()
}

func backgroundAgentCompletionMessage(taskID, agentName, status, content string, finishedAt time.Time) openai.Message {
	content = strings.TrimSpace(content)
	if content == "" {
		content = "No textual result."
	}
	attrs := fmt.Sprintf("task_id=\"%s\" agent=\"%s\" status=\"%s\"", taskID, agentName, status)
	if !finishedAt.IsZero() {
		attrs += " finished_at=\"" + finishedAt.UTC().Format(time.RFC3339Nano) + "\""
	}
	note := "<background_agent_completion " + attrs + ">\n" + content + "\n</background_agent_completion>"
	return openai.Message{Role: "user", Content: note}
}

func backgroundAgentCompletionRecorded(messages []openai.Message, taskID string, finishedAt time.Time) bool {
	taskMarker := `task_id="` + taskID + `"`
	finishedMarker := ""
	if !finishedAt.IsZero() {
		finishedMarker = `finished_at="` + finishedAt.UTC().Format(time.RFC3339Nano) + `"`
	}
	for _, msg := range messages {
		if !strings.Contains(msg.Content, "<background_agent_completion ") || !strings.Contains(msg.Content, taskMarker) {
			continue
		}
		if finishedMarker == "" || strings.Contains(msg.Content, finishedMarker) {
			return true
		}
	}
	return false
}

// migrateLegacyBackgroundAgentCompletion upgrades the pre-finished_at marker
// in place. That suppresses one duplicate after upgrade without making every
// later resume of the same task id look already delivered forever.
func migrateLegacyBackgroundAgentCompletion(messages []openai.Message, taskID string, finishedAt time.Time) bool {
	if finishedAt.IsZero() {
		return false
	}
	taskMarker := `task_id="` + taskID + `"`
	finishedAttr := ` finished_at="` + finishedAt.UTC().Format(time.RFC3339Nano) + `"`
	for i := range messages {
		content := messages[i].Content
		if !strings.Contains(content, "<background_agent_completion ") || !strings.Contains(content, taskMarker) || strings.Contains(content, `finished_at="`) {
			continue
		}
		end := strings.Index(content, ">")
		if end < 0 {
			continue
		}
		messages[i].Content = content[:end] + finishedAttr + content[end:]
		return true
	}
	return false
}

func (m *ChatModel) queueBackgroundAgentCompletion(taskID, agentName, status, content string, finishedAt time.Time) {
	if m == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if backgroundAgentCompletionRecorded(m.history, taskID, finishedAt) || backgroundAgentCompletionRecorded(m.pendingBackgroundAgentMessages, taskID, finishedAt) {
		return
	}
	m.pendingBackgroundAgentMessages = append(m.pendingBackgroundAgentMessages, backgroundAgentCompletionMessage(taskID, agentName, status, content, finishedAt))
}

func (m *ChatModel) recoverPendingBackgroundAgentMessages() {
	if m == nil {
		return
	}
	for _, msg := range m.messages {
		panel := msg.Agent
		if msg.Kind != MsgAgent || panel == nil || !panel.Background || panel.FinishedAt.IsZero() {
			continue
		}
		status := ""
		switch strings.ToLower(strings.TrimSpace(panel.Status)) {
		case "completed":
			status = "completed"
		case "failed":
			status = "failed"
		case "killed", "canceled":
			status = "canceled"
		default:
			continue
		}
		if migrateLegacyBackgroundAgentCompletion(m.history, panel.TaskID, panel.FinishedAt) {
			m.invalidateContextUsage()
			continue
		}
		m.queueBackgroundAgentCompletion(panel.TaskID, panel.Name, status, panel.Output, panel.FinishedAt)
	}
}

func (m *ChatModel) deliverBackgroundAgentMessages() {
	if len(m.pendingBackgroundAgentMessages) == 0 {
		return
	}
	pending := append([]openai.Message(nil), m.pendingBackgroundAgentMessages...)
	m.pendingBackgroundAgentMessages = nil

	// On a fresh user turn, submit() has already appended the user's prompt.
	// Keep that prompt as the final instruction while placing detached-worker
	// completions immediately before it. During tool/goal continuations the last
	// protocol message is not a normal user prompt, so appending is correct.
	last := len(m.history) - 1
	if last >= 0 && m.history[last].Role == "user" && !strings.Contains(m.history[last].Content, "<background_agent_completion ") {
		currentPrompt := m.history[last]
		m.history = append(m.history[:last], pending...)
		m.history = append(m.history, currentPrompt)
		m.invalidateContextUsage()
		return
	}
	m.appendHistory(pending...)
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
func (m *ChatModel) maybeStartElapsedTick() uikit.Cmd {
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

func NewChat(ctx *AppContext) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Escribe un mensaje…   ( / comandos · ! bash · Enter enviar/dirigir · Alt+Enter seguimiento · Shift+Enter salto · Esc abortar · Alt+↑ recuperar cola )"
	ta.Prompt = "❯ "
	ta.CharLimit = 20_000
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	// MaxHeight históricamente limitaba las líneas lógicas, no sólo
	// la altura visible. Debe quedar ilimitado para aceptar documentos
	// multilinea completos; chatInputVisibleMaxHeight controla únicamente
	// cuántas filas ocupa la caja en pantalla.
	ta.MaxHeight = 0
	ta.Focus()
	ta.FocusedStyle.CursorLine = tuistyle.NewStyle()
	ta.FocusedStyle.Prompt = tuistyle.NewStyle().Foreground(ctx.Styles.Theme.Primary)
	ta.FocusedStyle.Text = tuistyle.NewStyle().Foreground(ctx.Styles.Theme.Foreground)
	ta.BlurredStyle.Prompt = tuistyle.NewStyle().Foreground(ctx.Styles.Theme.Muted)

	vp := viewport.New(80, 20)
	vp.SetContent("")

	project, _ := os.Getwd()
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	m := &ChatModel{
		ctx:                ctx,
		viewport:           vp,
		textarea:           ta,
		mode:               ModeDefault,
		store:              session.NewStore(ctx.ConfigDir),
		rewindStore:        rewind.NewStore(ctx.ConfigDir),
		rewindTurn:         &rewindTurnState{},
		project:            project,
		codeIntel:          codeintel.New(project, ctx.ConfigDir),
		sess:               session.New(project),
		todos:              litodo.NewManager(nil),
		plans:              planstate.NewManager(nil),
		goals:              ligoal.NewManager(nil),
		planQuestion:       newPlanQuestionDock(ctx),
		sessionCtx:         sessionCtx,
		sessionCancel:      sessionCancel,
		agentEventCh:       make(chan agentEventEnvelope, 512),
		assistantActive:    -1,
		networkNoticeIndex: -1,
		contextCacheDirty:  true,
	}
	m.agentGeneration.Store(1)
	m.loadAgents()
	m.syncAgentModePresentation()
	m.runSessionHook("SessionStart")
	return m
}

// beginTurn snapshots the provider/model selected at the moment the user starts
// a request and creates one cancellation root shared by provider streaming and
// every tool spawned by that request. A model change made later therefore takes
// effect on the next user request, never halfway through a tool continuation.
func (m *ChatModel) beginTurn() error {
	return m.beginTurnMode(m.selectedAgentMode())
}

func (m *ChatModel) beginTurnMode(mode planstate.Mode) error {
	active := m.ctx.Providers.Active()
	if active.ProviderID == "" || active.ModelID == "" || m.ctx.Providers.FindProvider(active.ProviderID) == nil {
		return errors.New("no hay un proveedor/modelo activo; usa /login o /models")
	}
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.turnSeq++
	m.activeTurnID = m.turnSeq
	m.turnProvider = active.ProviderID
	m.turnModel = active.ModelID
	m.turnAgentMode = mode
	m.turnReasoningEffort = ""
	m.turnDeniedTools = nil
	m.turnSkillHooks = nil
	m.turnGoalManaged = m.goals != nil && m.goals.Active()
	if m.plans != nil {
		m.plans.BeginUserTurn(m.turnAgentMode)
		m.turnPlanHandoff = ""
		if m.turnAgentMode == planstate.Build {
			if plan, ok := m.plans.ConsumeBuildHandoff(); ok {
				m.turnPlanHandoff = plan
			}
		}
	}
	m.turnCtx, m.cancel = context.WithCancel(context.Background())
	m.streaming = true
	return nil
}

// endTurn releases the root context after a normal completion/error and makes
// any message still in flight from the old turn stale.
func (m *ChatModel) endTurn() {
	m.stopCompactionState()
	m.clearNetworkNotice()
	// Invalidar IDs antes de cancelar evita que cualquier evento que ya estaba
	// encolado en el runtime TUI pueda confundirse con trabajo aún vigente.
	m.activeRequestID = 0
	m.requestMessageStart = 0
	m.networkNoticeIndex = -1
	m.activeTurnID = 0
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel = nil
	m.turnCtx = nil
	m.turnProvider = ""
	m.turnModel = ""
	m.turnAgentMode = ""
	m.turnPlanHandoff = ""
	m.turnReasoningEffort = ""
	m.turnDeniedTools = nil
	m.turnSkillHooks = nil
	m.turnGoalManaged = false
	m.runningCalls = nil
}

func (m *ChatModel) checkpointPartialAssistantHistory() {
	text := m.streamBuf.String()
	reasoning := m.reasoningBuf.String()
	if strings.TrimSpace(text) != "" {
		m.appendHistory(openai.Message{Role: "assistant", Content: text, ReasoningContent: reasoning})
	}
	m.streamBuf.Reset()
	m.reasoningBuf.Reset()
}

// cancelTurn is deliberately cheap on the TUI state goroutine. It only
// signals cancellation, invalidates the turn, repairs tool-call history and
// refreshes the small mutable tail. Process-tree termination happens in the
// background command goroutine through the shared context.
func (m *ChatModel) cancelTurn() string {
	if m.activeTurnID == 0 {
		return ""
	}
	m.stopCompactionState()
	// Preserve any text already received from the current provider request in
	// the protocol history before invalidating the turn. Partial tool arguments
	// remain transcript-only because sending an unfinished call back to the API
	// would be invalid.
	m.checkpointPartialAssistantHistory()
	m.clearNetworkNotice()
	m.requestMessageStart = 0
	// Invalidate FIRST. A provider chunk, tool result or canceled-request event
	// that was already waiting in the TUI queue must become stale before we even
	// signal the OS. This is the hard guarantee that a process closing later can
	// never re-enter runTurn().
	m.activeRequestID = 0
	m.activeTurnID = 0
	m.turnGoalManaged = false
	// Mantener streaming=true sólo hasta refrescar el transcript permite reutilizar
	// el prefijo cacheado de conversaciones largas. El indicador ya desaparece
	// porque thinking/working se limpian inmediatamente.
	m.thinking = false
	m.working = false
	m.assistantActive = -1

	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel = nil
	m.turnCtx = nil

	// Only runningCalls already have a matching assistant tool_call in history.
	// Add synthetic outputs so the next request remains OpenAI-compatible.
	if len(m.runningCalls) > 0 {
		for _, c := range m.runningCalls {
			m.appendHistory(toolMessage(c, "cancelado por el usuario."))
		}
	}
	m.runningCalls = nil
	m.pendingCall = nil

	for _, p := range m.livePanels {
		if p != nil && !p.Done {
			p.Cancel()
		}
	}
	for _, cp := range m.cmdPanels {
		if cp != nil && !cp.Done {
			cp.Cancel()
		}
	}
	m.finishThinkingPanel()

	goalWasActive := m.goals != nil && m.goals.Active()
	m.pauseGoalOnInterrupt()
	notice := "Tarea cancelada."
	if goalWasActive {
		notice += " Goal pausado; usa /goal resume para continuarlo."
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: notice, Time: time.Now()})

	// Cancellation writes only the mutable checkpoint synchronously. The completed
	// history was already saved at semantic boundaries, so cancellation stays
	// instant even in very long conversations while still surviving an
	// immediate process exit.
	m.forceLivePersist()
	// Refresh only the mutable tail while streaming aún conserva la caché del
	// prefijo estable; después sí cerramos el estado de streaming.
	m.refreshTranscript(true)
	m.streaming = false
	m.turnProvider = ""
	m.turnModel = ""
	m.turnAgentMode = ""
	m.turnPlanHandoff = ""
	return notice
}

func cloneHistoryMessages(in []openai.Message) []openai.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]openai.Message, len(in))
	copy(out, in)
	for i := range out {
		if len(in[i].ToolCalls) > 0 {
			out[i].ToolCalls = append([]openai.ToolCall(nil), in[i].ToolCalls...)
		}
	}
	return out
}

// persist guarda un snapshot estable completo. Además del historial API
// protocol-correcto conserva el transcript visual, de modo que una sesión
// cancelada puede volver a mostrar razonamiento, paneles y avisos exactamente
// como estaban. Los checkpoints de streaming viven en un sidecar separado.
func (m *ChatModel) persist() {
	if m.store == nil || m.sess == nil {
		return
	}
	m.persistRevision++
	m.sess.Messages = cloneHistoryMessages(m.history)
	m.sess.Transcript = m.snapshotTranscriptRange(0, len(m.messages))
	m.sess.Todo = m.todoStatePointer()
	m.sess.Plan = m.planStatePointer()
	m.sess.Goal = m.goalStatePointer()
	m.sess.Revision = m.persistRevision
	if err := m.store.Save(m.sess); err != nil {
		return
	}
	m.liveBaseMessageCount = len(m.messages)
	m.liveBaseHistoryCount = len(m.history)
	m.livePersistDirty = false
	_ = m.store.ClearLive(m.project, m.sess.ID, m.sess.Revision)
	// El snapshot estable ya absorbió cualquier checkpoint que pudiera venir
	// de LoadSession. No conservar un puntero Live obsoleto en memoria.
	m.sess.Live = nil
}

// persistTurnStart makes admitting a new prompt cheap on long sessions. A
// brand-new session still gets one full base snapshot so crash recovery has a
// durable anchor; subsequent turns only append the small live tail here. The
// full stable snapshot is already written when the previous turn completes.
func (m *ChatModel) persistTurnStart() {
	if m.store == nil || m.sess == nil {
		return
	}
	if len(m.sess.Messages) == 0 && m.liveBaseHistoryCount == 0 {
		m.persist()
		return
	}
	m.forceLivePersist()
}

func transcriptKindName(kind MessageKind) string {
	switch kind {
	case MsgUser:
		return "user"
	case MsgAssistant:
		return "assistant"
	case MsgSystem:
		return "system"
	case MsgError:
		return "error"
	case MsgTool:
		return "tool"
	case MsgFile:
		return "file"
	case MsgCommand:
		return "command"
	case MsgThinking:
		return "thinking"
	case MsgAgent:
		return "agent"
	default:
		return "system"
	}
}

func messageKindFromName(name string) MessageKind {
	switch name {
	case "user":
		return MsgUser
	case "assistant":
		return MsgAssistant
	case "error":
		return MsgError
	case "tool":
		return MsgTool
	case "file":
		return MsgFile
	case "command":
		return MsgCommand
	case "thinking":
		return MsgThinking
	case "agent":
		return MsgAgent
	default:
		return MsgSystem
	}
}

func (m *ChatModel) snapshotTranscriptRange(start, end int) []session.TranscriptEntry {
	if start < 0 {
		start = 0
	}
	if end > len(m.messages) {
		end = len(m.messages)
	}
	if start >= end {
		return nil
	}
	out := make([]session.TranscriptEntry, 0, end-start)
	for _, msg := range m.messages[start:end] {
		e := session.TranscriptEntry{
			Kind:    transcriptKindName(msg.Kind),
			Content: msg.Content,
			Time:    msg.Time,
		}
		if p := msg.Panel; p != nil {
			fp := &session.FileProgress{
				Tool: p.Tool, CallID: p.CallID, Index: p.Index, Path: p.Path,
				Content: p.Content, Old: p.Old, New: p.New, Done: p.Done,
				Failed: p.Failed, Skipped: p.Skipped, Canceled: p.Canceled,
				Superseded: p.Superseded, Result: p.Result, Expanded: p.Expanded,
			}
			if len(p.Edits) > 0 {
				fp.Edits = make([]session.TextEdit, 0, len(p.Edits))
				for _, edit := range p.Edits {
					fp.Edits = append(fp.Edits, session.TextEdit{Old: edit.Old, New: edit.New})
				}
			}
			e.File = fp
		}
		if cp := msg.Command; cp != nil {
			e.Command = &session.CommandProgress{
				CallID: cp.CallID, Index: cp.Index, Command: cp.Command, Timeout: cp.Timeout,
				Done: cp.Done, Failed: cp.Failed, Superseded: cp.Superseded,
				ExitCode: cp.ExitCode, Stdout: cp.Stdout, Stderr: cp.Stderr,
				TimedOut: cp.TimedOut, Canceled: cp.Canceled, StartedAt: cp.StartedAt,
				Elapsed: cp.Elapsed, Expanded: cp.Expanded,
			}
		}
		if tp := msg.Thinking; tp != nil {
			e.Thinking = &session.ThinkingProgress{Content: tp.Content, Done: tp.Done, Expanded: tp.Expanded}
		}
		if ap := msg.Agent; ap != nil {
			progress := &session.AgentProgress{
				TaskID: ap.TaskID, ParentTaskID: ap.ParentTaskID, Name: ap.Name, Description: ap.Description,
				Model: ap.Model, Depth: ap.Depth, Resumed: ap.Resumed, Background: ap.Background, Status: ap.Status,
				StartedAt: ap.StartedAt, FinishedAt: ap.FinishedAt, Reasoning: ap.Reasoning, Output: ap.Output, Expanded: ap.Expanded,
			}
			for _, a := range ap.Activities {
				progress.Activities = append(progress.Activities, session.AgentActivityProgress{
					CallID: a.CallID, Name: a.Name, Args: a.Args, Result: a.Result, Running: a.Running, Failed: a.Failed, Started: a.Started, Finished: a.Finished,
				})
			}
			e.Agent = progress
		}
		out = append(out, e)
	}
	return out
}

// requestLivePersist coalesces token-level changes into a compact sidecar at
// most five times per second. Disk IO runs as a TUI command, never on
// the Update goroutine, so long chats do not become laggy again.
func (m *ChatModel) requestLivePersist() uikit.Cmd {
	if m.store == nil || m.sess == nil || m.activeTurnID == 0 {
		return nil
	}
	if m.livePersistPending {
		m.livePersistDirty = true
		return nil
	}
	if !m.lastLivePersist.IsZero() {
		elapsed := time.Since(m.lastLivePersist)
		if elapsed < livePersistInterval {
			m.livePersistDirty = true
			if m.livePersistTimer {
				return nil
			}
			m.livePersistTimer = true
			wait := livePersistInterval - elapsed
			return uikit.Tick(wait, func(time.Time) uikit.Msg { return livePersistTickMsg{} })
		}
	}
	return m.startLivePersist()
}

func (m *ChatModel) startLivePersist() uikit.Cmd {
	if m.store == nil || m.sess == nil {
		m.livePersistDirty = false
		return nil
	}
	entries := m.snapshotTranscriptRange(m.liveBaseMessageCount, len(m.messages))
	historyStart := m.liveBaseHistoryCount
	if historyStart < 0 {
		historyStart = 0
	}
	if historyStart > len(m.history) {
		historyStart = len(m.history)
	}
	if len(entries) == 0 && historyStart == len(m.history) {
		m.livePersistDirty = false
		return nil
	}
	m.persistRevision++
	revision := m.persistRevision
	checkpoint := &session.LiveCheckpoint{
		Revision: revision, BaseTranscriptCount: m.liveBaseMessageCount,
		BaseHistoryCount: historyStart, UpdatedAt: time.Now(), Entries: entries,
		History: cloneHistoryMessages(m.history[historyStart:]),
		Todo:    m.todoStatePointer(),
		Plan:    m.planStatePointer(),
		Goal:    m.goalStatePointer(),
	}
	store := m.store
	project := m.project
	sessionID := m.sess.ID
	m.livePersistPending = true
	m.livePersistDirty = false
	m.livePersistTimer = false
	m.lastLivePersist = time.Now()
	return func() uikit.Msg {
		err := store.SaveLive(project, sessionID, checkpoint)
		return livePersistDoneMsg{revision: revision, err: err}
	}
}

// forceLivePersist writes the small mutable checkpoint synchronously. It is
// used by cancellation because the user may exit immediately afterwards. Unlike a
// full session save it never rewrites the completed chat history.
func (m *ChatModel) forceLivePersist() {
	if m.store == nil || m.sess == nil {
		return
	}
	entries := m.snapshotTranscriptRange(m.liveBaseMessageCount, len(m.messages))
	historyStart := m.liveBaseHistoryCount
	if historyStart < 0 {
		historyStart = 0
	}
	if historyStart > len(m.history) {
		historyStart = len(m.history)
	}
	// Even when transcript/history have no new entries, a lightweight sidecar
	// may still be required to persist session state such as the Build/Plan/Goal
	// selection changed with Tab during a running turn.
	m.persistRevision++
	checkpoint := &session.LiveCheckpoint{
		Revision: m.persistRevision, BaseTranscriptCount: m.liveBaseMessageCount,
		BaseHistoryCount: historyStart, UpdatedAt: time.Now(), Entries: entries,
		History: cloneHistoryMessages(m.history[historyStart:]),
		Todo:    m.todoStatePointer(),
		Plan:    m.planStatePointer(),
		Goal:    m.goalStatePointer(),
	}
	_ = m.store.SaveLive(m.project, m.sess.ID, checkpoint)
	m.lastLivePersist = time.Now()
}

func (m *ChatModel) invalidateContextUsage() {
	m.contextCacheDirty = true
}

func (m *ChatModel) appendHistory(msgs ...openai.Message) {
	if len(msgs) == 0 {
		return
	}
	m.history = append(m.history, msgs...)
	m.invalidateContextUsage()
}

func (m *ChatModel) restoreTranscriptEntries(entries []session.TranscriptEntry, interrupted bool) {
	for _, e := range entries {
		msg := ChatMessage{Kind: messageKindFromName(e.Kind), Content: e.Content, Time: e.Time}
		if msg.Kind == MsgTool {
			plain := strings.TrimSpace(e.Content)
			if strings.HasPrefix(plain, "$ plan_question") || strings.HasPrefix(plain, "$ plan_exit") || strings.HasPrefix(plain, "$ todo_write") {
				continue
			}
		}
		if e.File != nil {
			fp := e.File
			p := &FilePanel{
				Tool: fp.Tool, CallID: fp.CallID, Index: fp.Index, Path: fp.Path,
				Content: fp.Content, Old: fp.Old, New: fp.New, Done: fp.Done,
				Failed: fp.Failed, Skipped: fp.Skipped, Canceled: fp.Canceled,
				Superseded: fp.Superseded, Result: fp.Result, Expanded: fp.Expanded,
			}
			for _, edit := range fp.Edits {
				p.Edits = append(p.Edits, filePanelEdit{Old: edit.Old, New: edit.New})
			}
			if interrupted && !p.Done {
				p.Cancel()
				p.Result = "interrumpido antes de completar la herramienta"
			}
			msg.Kind = MsgFile
			msg.Panel = p
			m.livePanels[p.Index] = p
			if p.CallID != "" {
				m.panelByCall[p.CallID] = p
			}
		}
		if e.Command != nil {
			cp := e.Command
			p := &CommandPanel{
				CallID: cp.CallID, Index: cp.Index, Command: cp.Command, Timeout: cp.Timeout,
				Done: cp.Done, Failed: cp.Failed, Superseded: cp.Superseded,
				ExitCode: cp.ExitCode, Stdout: cp.Stdout, Stderr: cp.Stderr,
				TimedOut: cp.TimedOut, Canceled: cp.Canceled, StartedAt: cp.StartedAt,
				Elapsed: cp.Elapsed, Expanded: cp.Expanded,
			}
			if interrupted && !p.Done {
				p.Cancel()
			}
			msg.Kind = MsgCommand
			msg.Command = p
			m.cmdPanels[p.Index] = p
			if p.CallID != "" {
				m.cmdByCall[p.CallID] = p
			}
		}
		if e.Thinking != nil {
			tp := e.Thinking
			p := &ThinkingPanel{Content: tp.Content, Done: tp.Done, Expanded: tp.Expanded}
			if interrupted {
				p.Done = true
			}
			msg.Kind = MsgThinking
			msg.Thinking = p
		}
		if e.Agent != nil {
			ap := e.Agent
			p := &AgentPanel{
				TaskID: ap.TaskID, ParentTaskID: ap.ParentTaskID, Name: ap.Name, Description: ap.Description, Model: ap.Model,
				Depth: ap.Depth, Resumed: ap.Resumed, Background: ap.Background, Status: ap.Status, StartedAt: ap.StartedAt, FinishedAt: ap.FinishedAt,
				Reasoning: ap.Reasoning, Output: ap.Output, Expanded: ap.Expanded,
			}
			for _, a := range ap.Activities {
				p.Activities = append(p.Activities, AgentActivity{CallID: a.CallID, Name: a.Name, Args: a.Args, Result: a.Result, Running: a.Running, Failed: a.Failed, Started: a.Started, Finished: a.Finished})
			}
			if interrupted && p.Status == "running" {
				p.Status = "killed"
				p.FinishedAt = time.Now()
				for i := range p.Activities {
					p.Activities[i].Running = false
				}
			}
			msg.Kind = MsgAgent
			msg.Agent = p
			if m.agentPanels == nil {
				m.agentPanels = map[string]*AgentPanel{}
			}
			if p.TaskID != "" {
				m.agentPanels[p.TaskID] = p
			}
		}
		m.messages = append(m.messages, msg)
	}
}

func (m *ChatModel) recoverLiveAssistantHistory(entries []session.TranscriptEntry) {
	var reasoning string
	for _, e := range entries {
		if e.Thinking != nil && strings.TrimSpace(e.Thinking.Content) != "" {
			reasoning = e.Thinking.Content
			continue
		}
		if e.Kind != "assistant" || strings.TrimSpace(e.Content) == "" {
			continue
		}
		m.appendHistory(openai.Message{Role: "assistant", Content: e.Content, ReasoningContent: reasoning})
		reasoning = ""
	}
}

func (m *ChatModel) repairDanglingToolHistory() bool {
	outputs := make(map[string]bool)
	for _, msg := range m.history {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			outputs[msg.ToolCallID] = true
		}
	}
	var missing []openai.Message
	for _, msg := range m.history {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.ID == "" || outputs[call.ID] {
				continue
			}
			missing = append(missing, toolMessage(call, "cancelado: la sesión anterior terminó antes de completar la herramienta."))
			outputs[call.ID] = true
		}
	}
	if len(missing) == 0 {
		return false
	}
	m.appendHistory(missing...)
	return true
}

// LoadSession reemplaza la conversación activa por una guardada y reconstruye
// el transcript visible a partir del historial real. Las tool calls de
// archivo (create_file / write_file / append_file / str_replace; `write` en sesiones antiguas) se rehidratan como FilePanel para que
// la sesión reanudada conserve el mismo diseño que cuando se estaban
// ejecutando en vivo, en lugar de degradarse a una línea de texto genérica.
func (m *ChatModel) LoadSession(s *session.Session) {
	if s == nil {
		return
	}
	m.Clear()
	m.sess = s
	m.project = filepath.Clean(s.ProjectPath)
	m.clearActiveRewindPoint()
	m.history = s.Messages
	m.todoExpanded = false
	if m.todos == nil {
		m.todos = litodo.NewManager(s.Todo)
	} else if err := m.todos.Restore(s.Todo); err != nil {
		m.todos.Reset()
	}
	if m.plans == nil {
		m.plans = planstate.NewManager(s.Plan)
	} else if err := m.plans.Restore(s.Plan); err != nil {
		m.plans.Reset()
	}
	if m.goals == nil {
		m.goals = ligoal.NewManager(s.Goal)
	} else {
		m.goals = ligoal.NewManager(s.Goal)
	}
	m.goalRequestTokens = 0
	m.syncAgentModePresentation()
	m.invalidateContextUsage()
	m.livePanels = map[int]*FilePanel{}
	m.panelByCall = map[string]*FilePanel{}
	m.cmdPanels = map[int]*CommandPanel{}
	m.cmdByCall = map[string]*CommandPanel{}
	m.agentPanels = map[string]*AgentPanel{}
	m.persistRevision = s.Revision

	// Sesiones nuevas guardan un transcript independiente del historial API.
	// Así podemos restaurar razonamiento y paneles parciales sin convertir una
	// tool call incompleta en un mensaje inválido para el proveedor.
	if len(s.Transcript) > 0 {
		m.restoreTranscriptEntries(s.Transcript, true)
		recoveredLive := false
		if s.Live != nil && s.Live.Revision > s.Revision {
			if s.Live.Todo != nil {
				_ = m.todos.Restore(s.Live.Todo)
			}
			if s.Live.Plan != nil {
				_ = m.plans.Restore(s.Live.Plan)
				m.syncAgentModePresentation()
			}
			if s.Live.Goal != nil {
				m.goals = ligoal.NewManager(s.Live.Goal)
			}
			m.restoreTranscriptEntries(s.Live.Entries, true)
			appendedLiveHistory := false
			base := s.Live.BaseHistoryCount
			if base >= 0 && base <= len(m.history) {
				skip := len(m.history) - base
				if skip < len(s.Live.History) {
					m.appendHistory(s.Live.History[skip:]...)
					appendedLiveHistory = true
				}
			}
			// A hard process crash can happen before a partial assistant delta was
			// promoted into protocol history. Recover plain assistant text safely;
			// unfinished tool calls remain transcript-only.
			if !appendedLiveHistory {
				m.recoverLiveAssistantHistory(s.Live.Entries)
			}
			m.persistRevision = s.Live.Revision
			recoveredLive = true
		}
		repaired := m.repairDanglingToolHistory()
		m.recoverPendingBackgroundAgentMessages()
		if recoveredLive || repaired {
			// Promote the recovered checkpoint to a stable snapshot immediately;
			// the stale sidecar is removed and future requests see repaired tool
			// outputs rather than an incomplete protocol sequence.
			m.persist()
		} else {
			m.liveBaseMessageCount = len(m.messages)
			m.liveBaseHistoryCount = len(m.history)
		}
		if panels := m.panels(); len(panels) > 0 {
			m.panelSel = len(panels) - 1
		}
		m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: "Sesión reanudada: " + s.Title, Time: time.Now()})
		if m.ctx.Width > 0 && m.ctx.Height > 0 {
			m.Resize(m.ctx.Width, m.ctx.Height)
		} else {
			m.refreshTranscript(true)
		}
		return
	}
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
			if strings.TrimSpace(msg.ReasoningContent) != "" {
				m.messages = append(m.messages, ChatMessage{
					Kind: MsgThinking,
					Thinking: &ThinkingPanel{
						Content:  msg.ReasoningContent,
						Done:     true,
						Expanded: false,
					},
					Time: s.UpdatedAt,
				})
			}
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
					if isTodoToolName(c.Function.Name) || isPlanQuestionToolName(c.Function.Name) || isPlanExitToolName(c.Function.Name) || isAgentToolName(c.Function.Name) {
						continue
					}
					m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: describeCall(c), Time: s.UpdatedAt})
				}
			}
		case "tool":
			if (isTodoToolName(msg.Name) || isPlanQuestionToolName(msg.Name) || isPlanExitToolName(msg.Name) || isAgentToolName(msg.Name)) &&
				!strings.HasPrefix(strings.TrimSpace(msg.Content), "error:") {
				continue
			}
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
	m.recoverPendingBackgroundAgentMessages()
	if m.repairDanglingToolHistory() {
		m.persist()
	} else {
		m.liveBaseMessageCount = len(m.messages)
		m.liveBaseHistoryCount = len(m.history)
	}
	// Añadimos el aviso antes de un único refresh: AddSystem refrescaría por sí
	// solo y en una sesión larga eso duplicaría el render completo al reanudar.
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: "Sesión reanudada: " + s.Title, Time: time.Now()})
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	} else {
		m.refreshTranscript(true)
	}
}

// chatUsableWidth reserves the final terminal column for the transcript
// scrollbar. Every bottom-chrome component must fit inside this exact total
// width; Style.Width configures CONTENT width and therefore must subtract its
// own border and padding separately.
func chatUsableWidth(w int) int {
	if w > 1 {
		return w - 1
	}
	if w < 1 {
		return 1
	}
	return w
}

func chatBorderedContentWidth(w int) int {
	width := chatUsableWidth(w) - 4 // 2 borders + horizontal padding 1+1
	if width < 1 {
		return 1
	}
	return width
}

func chatPaddedContentWidth(w int) int {
	width := chatUsableWidth(w) - 2 // horizontal padding 1+1
	if width < 1 {
		return 1
	}
	return width
}

func (m *ChatModel) Resize(w, h int) {
	m.ctx.Width, m.ctx.Height = w, h
	if w > 5 {
		m.textarea.SetWidth(chatBorderedContentWidth(w))
	}
	m.setInputHeightForContent()
	used, maxCtx := m.contextUsage()
	vpHeight := m.viewportHeightForFrame(w, h, used, maxCtx)
	// Reservamos 1 columna a la derecha para la scrollbar vertical.
	vpWidth := w - 1
	if vpWidth < 10 {
		vpWidth = w
	}
	m.viewport.Width = vpWidth
	m.viewport.Height = vpHeight
	m.refreshTranscript(true)
}

func (m *ChatModel) resetProviderAttemptForRetry() {
	start := m.requestMessageStart
	if start < 0 || start > len(m.messages) {
		start = len(m.messages)
	}
	// Provider output can be interleaved with unrelated runtime notices or
	// background-agent events. Remove only presentation created by this HTTP/SSE
	// attempt; never truncate those independent messages.
	kept := append([]ChatMessage(nil), m.messages[:start]...)
	for idx, message := range m.messages[start:] {
		absIdx := start + idx
		if absIdx == m.networkNoticeIndex {
			continue
		}
		switch message.Kind {
		case MsgAssistant, MsgThinking, MsgFile, MsgCommand:
			continue
		default:
			kept = append(kept, message)
		}
	}
	m.messages = kept
	m.requestMessageStart = len(m.messages)
	m.streamBuf.Reset()
	m.reasoningBuf.Reset()
	m.pendingCall = nil
	m.livePanels = nil
	m.panelByCall = nil
	m.cmdPanels = nil
	m.cmdByCall = nil
	if panels := m.panels(); len(panels) == 0 {
		m.panelSel = 0
		m.panelPinned = false
	} else if m.panelSel < 0 || m.panelSel >= len(panels) {
		m.panelSel = len(panels) - 1
		m.panelPinned = false
	}
	m.thinkingActive = nil
	m.assistantActive = -1
	m.networkNoticeIndex = -1
	m.thinking = true
	m.working = false
	m.invalidateTranscriptCache()
}

func (m *ChatModel) showNetworkRetry(status openai.RetryStatus) {
	providerName := strings.TrimSpace(m.turnProvider)
	if provider := m.ctx.Providers.FindProvider(m.turnProvider); provider != nil && strings.TrimSpace(provider.Name) != "" {
		providerName = strings.TrimSpace(provider.Name)
	}
	if providerName == "" {
		providerName = "el proveedor"
	}

	var text string
	switch status.State {
	case openai.ConnectivityOffline:
		text = "Sin conexión a Internet. Lilith conserva este turno y reintentará automáticamente"
	case openai.ConnectivityProviderUnavailable:
		text = providerName + " no responde aunque hay conexión. Lilith reintentará automáticamente"
	default:
		text = "La conexión se interrumpió. Lilith está comprobando la red para reintentar automáticamente"
	}
	if status.After > 0 {
		seconds := int((status.After + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		text += fmt.Sprintf(" en %d s", seconds)
	}
	text += ". Pulsa Esc para cancelar."

	if m.networkNoticeIndex >= 0 && m.networkNoticeIndex < len(m.messages) && m.messages[m.networkNoticeIndex].Kind == MsgSystem {
		m.messages[m.networkNoticeIndex].Content = text
		m.messages[m.networkNoticeIndex].Time = time.Now()
		return
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: text, Time: time.Now()})
	m.networkNoticeIndex = len(m.messages) - 1
}

func (m *ChatModel) clearNetworkNotice() {
	idx := m.networkNoticeIndex
	m.networkNoticeIndex = -1
	if idx < 0 || idx >= len(m.messages) || m.messages[idx].Kind != MsgSystem {
		return
	}
	copy(m.messages[idx:], m.messages[idx+1:])
	m.messages = m.messages[:len(m.messages)-1]
	if m.assistantActive > idx {
		m.assistantActive--
	}
	m.invalidateTranscriptCache()
}

func (m *ChatModel) AddSystem(text string) {
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: text, Time: time.Now()})
	m.refreshTranscript(true)
}

func (m *ChatModel) AddError(text string) {
	m.messages = append(m.messages, ChatMessage{Kind: MsgError, Content: text, Time: time.Now()})
	m.refreshTranscript(true)
}

// finalizeRunningAgentPanels closes the visual lifecycle before a session
// boundary. Events from the canceled workers are intentionally discarded by
// the next generation, so the old session itself must not remain persisted as
// "running" forever when it is reopened later.
func (m *ChatModel) finalizeRunningAgentPanels(reason string) bool {
	if m == nil || len(m.agentPanels) == 0 {
		return false
	}
	now := time.Now()
	changed := false
	for _, panel := range m.agentPanels {
		if panel == nil || !strings.EqualFold(strings.TrimSpace(panel.Status), "running") {
			continue
		}
		panel.Apply(subagents.Event{Kind: subagents.EventCanceled, TaskID: panel.TaskID, AgentName: panel.Name, Background: panel.Background, At: now})
		if note := strings.TrimSpace(reason); note != "" {
			panel.Output = appendAgentLine(panel.Output, note)
		}
		changed = true
	}
	return changed
}

func (m *ChatModel) Clear() {
	m.endTurn()
	if m.finalizeRunningAgentPanels("Cancelado al cerrar o cambiar la sesión.") {
		m.persist()
	}
	m.resetAgentSessionContext()
	m.streaming = false
	m.thinking = false
	m.working = false
	m.messages = nil
	m.history = nil
	m.invalidateContextUsage()
	m.activeTools = nil
	m.buildToolCache = nil
	m.planToolCache = nil
	m.pendingCall = nil
	m.toolFallback = ""
	m.livePanels = nil
	m.panelByCall = nil
	m.cmdPanels = nil
	m.cmdByCall = nil
	m.agentPanels = nil
	m.pendingBackgroundAgentMessages = nil
	m.panelSel = 0
	m.panelPinned = false
	m.thinkingActive = nil
	m.assistantActive = -1
	m.reasoningBuf.Reset()
	m.clearActiveRewindPoint()
	m.todoExpanded = false
	m.userScrolled = false
	m.sess = session.New(m.project)
	if m.todos == nil {
		m.todos = litodo.NewManager(nil)
	} else {
		m.todos.Reset()
	}
	if m.plans == nil {
		m.plans = planstate.NewManager(nil)
	} else {
		m.plans.Reset()
	}
	if m.goals == nil {
		m.goals = ligoal.NewManager(nil)
	} else {
		m.goals.Clear()
	}
	m.goalRequestTokens = 0
	m.autoCompactionSkipHistoryLen = 0
	m.planQuestion.resetPresentation()
	m.syncAgentModePresentation()
	m.liveBaseMessageCount = 0
	m.liveBaseHistoryCount = 0
	m.persistRevision = 0
	m.livePersistPending = false
	m.livePersistDirty = false
	m.livePersistTimer = false
	m.lastLivePersist = time.Time{}
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	} else {
		m.refreshTranscript(false)
	}
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

// ensureAssistantMessage materializa la respuesta textual del paso actual sólo
// cuando el proveedor ya emitió contenido real. Así `Pensando…` puede estar
// visible durante latencia/red/reintentos sin una cabecera `lilith` vacía.
func (m *ChatModel) ensureAssistantMessage() int {
	if m.assistantActive >= 0 && m.assistantActive < len(m.messages) && m.messages[m.assistantActive].Kind == MsgAssistant {
		return m.assistantActive
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Time: time.Now()})
	m.assistantActive = len(m.messages) - 1
	return m.assistantActive
}

func (m *ChatModel) finishThinkingPanel() {
	if m.thinkingActive == nil {
		return
	}
	m.thinkingActive.Finish()
	m.thinkingActive = nil
}

// applyToolCalls crea o refresca las ventanas en vivo (archivo o comando)
// con los argumentos (posiblemente parciales) que el modelo lleva emitidos.
func (m *ChatModel) applyToolCalls(calls []openai.ToolCall) {
	previousPanelSel := m.panelSel
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
	if m.panelSel != previousPanelSel {
		m.invalidateTranscriptCache()
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
	dir := s.Muted.Render("Directorio ") + tuistyle.NewStyle().Foreground(s.Theme.Primary).Render(cwd)
	return tuistyle.JoinVertical(tuistyle.Left, logo, "", tag, dir, "")
}

func (m *ChatModel) selectedFilePanel() *FilePanel {
	all := m.panels()
	if m.panelSel < 0 || m.panelSel >= len(all) {
		return nil
	}
	return all[m.panelSel]
}

func (m *ChatModel) renderMessage(msg ChatMessage, width int, selectedPanel *FilePanel) string {
	s := m.ctx.Styles
	ts := msg.Time.Format("15:04")
	stamp := s.Muted.Render("[" + ts + "]")
	var b strings.Builder

	switch msg.Kind {
	case MsgUser:
		b.WriteString(stamp + " " + s.MessageUser.Render("tú"))
		b.WriteString("\n")
		b.WriteString(indent(msg.Content, "  "))
	case MsgAssistant:
		b.WriteString(stamp + " " + s.Accent.Render("» lilith"))
		b.WriteString("\n")
		rendered := RenderMarkdown(msg.Content, width-2)
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
		// for results, so we render them verbatim in a muted terminal-ish tone.
		style := tuistyle.NewStyle().Foreground(s.Theme.Muted)
		if strings.HasPrefix(msg.Content, "$ ") {
			head := s.Accent.Render("$")
			rest := style.Render(strings.TrimPrefix(msg.Content, "$"))
			b.WriteString(head + rest)
		} else {
			b.WriteString(style.Render(msg.Content))
		}
	case MsgFile:
		if msg.Panel != nil {
			b.WriteString(msg.Panel.View(s, width, selectedPanel == msg.Panel))
		}
	case MsgCommand:
		if msg.Command != nil {
			b.WriteString(msg.Command.View(s, width, false))
		}
	case MsgThinking:
		if msg.Thinking != nil {
			b.WriteString(msg.Thinking.View(s, width, false))
		}
	case MsgAgent:
		if msg.Agent != nil {
			b.WriteString(msg.Agent.View(s, width))
		}
	}
	return b.String()
}

// renderTranscriptRange renderiza un tramo del transcript. El encabezado sólo
// se incluye para el prefijo inicial; así refreshTranscript puede conservar un
// bloque ya renderizado y recalcular únicamente el turno activo.
func (m *ChatModel) renderTranscriptRange(start, end int, includeHeader bool) string {
	if start < 0 {
		start = 0
	}
	if end > len(m.messages) {
		end = len(m.messages)
	}
	if end < start {
		end = start
	}

	w := m.ctx.Width - 4
	if w < 20 {
		w = 60
	}
	selectedPanel := m.selectedFilePanel()
	var b strings.Builder
	if includeHeader {
		b.WriteString(m.renderHeader())
		if start < end {
			b.WriteString("\n")
		}
	}
	for i := start; i < end; i++ {
		b.WriteString(m.renderMessage(m.messages[i], w, selectedPanel))
		if i < end-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func (m *ChatModel) renderTranscript() string {
	return m.renderTranscriptRange(0, len(m.messages), true)
}

func (m *ChatModel) invalidateTranscriptCache() {
	m.transcriptPrefixValid = false
}

// stableTranscriptPrefixCount devuelve la parte del transcript que no cambia
// durante el turno actual. Todo lo anterior e incluido el último mensaje del
// usuario es inmutable mientras llegan deltas, tool calls y paneles vivos.
func (m *ChatModel) stableTranscriptPrefixCount() int {
	if !m.streaming {
		return len(m.messages)
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Kind == MsgUser {
			return i + 1
		}
	}
	return 0
}

func wrapTranscriptChunk(content string, width int) string {
	if content == "" || width <= 0 {
		return content
	}
	return tuistyle.NewStyle().Width(width).Render(content)
}

func splitTranscriptLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func appendTranscriptLines(base []string, chunk string) []string {
	lines := splitTranscriptLines(chunk)
	if len(lines) == 0 {
		return base
	}
	if len(base) > 0 {
		base = append(base, "")
	}
	return append(base, lines...)
}

func (m *ChatModel) refreshTranscript(scrollBottom bool) {
	prefixCount := m.stableTranscriptPrefixCount()
	width := m.viewport.Width
	if !m.transcriptPrefixValid || m.transcriptPrefixWidth != width || prefixCount < m.transcriptPrefixCount {
		m.transcriptPrefixLines = splitTranscriptLines(wrapTranscriptChunk(
			m.renderTranscriptRange(0, prefixCount, true),
			width,
		))
		m.transcriptPrefixCount = prefixCount
		m.transcriptPrefixWidth = width
		m.transcriptPrefixValid = true
	} else if prefixCount > m.transcriptPrefixCount {
		// El historial estable sólo crece en condiciones normales. Renderiza
		// exclusivamente los mensajes nuevos y conserva sus filas ya procesadas;
		// así una conversación de cientos de mensajes no vuelve a concatenarse ni
		// dividirse completa durante cada delta del turno siguiente.
		chunk := wrapTranscriptChunk(
			m.renderTranscriptRange(m.transcriptPrefixCount, prefixCount, false),
			width,
		)
		m.transcriptPrefixLines = appendTranscriptLines(m.transcriptPrefixLines, chunk)
		m.transcriptPrefixCount = prefixCount
		m.transcriptPrefixWidth = width
	}

	var tailLines []string
	if prefixCount < len(m.messages) {
		tail := wrapTranscriptChunk(
			m.renderTranscriptRange(prefixCount, len(m.messages), false),
			width,
		)
		tailLines = splitTranscriptLines(tail)
		if len(m.transcriptPrefixLines) > 0 && len(tailLines) > 0 {
			// Equivale a unir prefijo y cola con dos saltos: queda una fila vacía entre
			// ambos bloques, sin reconstruir un string del transcript completo.
			tailLines = append([]string{""}, tailLines...)
		}
	}

	m.viewport.SetLineSegments(m.transcriptPrefixLines, tailLines)
	m.lastTranscriptRefresh = time.Now()
	m.transcriptRefreshPending = false
	m.transcriptRefreshAutoBottom = false
	// Sólo hacemos autoscroll cuando el usuario no ha subido manualmente. Así
	// puede leer historial mientras la CLI sigue ejecutando comandos, sin que
	// cada frame lo tire de vuelta al fondo.
	if scrollBottom && !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// refreshTranscriptStreaming limita el número de reconstrucciones del
// viewport durante SSE. Los mensajes y streamBuf se actualizan siempre; sólo
// agrupamos la pintura. Esto conserva el historial completo y evita que
// se reconstruyan miles de líneas por cada token recibido.
func (m *ChatModel) refreshTranscriptStreaming(scrollBottom bool) uikit.Cmd {
	if scrollBottom {
		m.transcriptRefreshAutoBottom = true
	}
	interval := transcriptRefreshInterval
	if m.userScrolled {
		// Mientras el usuario lee mensajes antiguos no necesita 20 repintados
		// por segundo del contenido que está naciendo debajo. Mantener una
		// cadencia más baja conserva actualizado el scrollbar sin castigar el
		// scroll de historiales grandes.
		interval = transcriptScrolledRefreshInterval
	}
	elapsed := time.Since(m.lastTranscriptRefresh)
	if m.lastTranscriptRefresh.IsZero() || elapsed >= interval {
		m.refreshTranscript(m.transcriptRefreshAutoBottom)
		return nil
	}
	if m.transcriptRefreshPending {
		return nil
	}
	m.transcriptRefreshPending = true
	wait := interval - elapsed
	return uikit.Tick(wait, func(time.Time) uikit.Msg { return transcriptRefreshTickMsg{} })
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func (m *ChatModel) Init() uikit.Cmd {
	return uikit.Batch(textarea.Blink, agentEventPump(m.agentEventCh), m.connectMCP(), m.resumeActiveGoalCmd())
}

// visualInputLineCount estima las filas visibles que ocupará el valor dentro
// del textarea. El conteo de líneas lógicas no incluye el soft-wrap: LineCount
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
		width := tuistyle.Width(row)
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
	// textarea.MaxHeight no se usa como límite visual porque históricamente también
	// lo aplica al contenido y descartaría todas las líneas posteriores.
	maxHeight := chatInputVisibleMaxHeight
	value := m.textarea.Value()
	textWidth := m.textarea.Width() - tuistyle.Width(m.textarea.Prompt)
	if textWidth < 1 {
		textWidth = 1
	}
	totalLines := visualInputLineCount(value, textWidth, 1_000_000)
	lines := totalLines
	if lines > maxHeight {
		lines = maxHeight
	}
	if lines == m.textarea.Height() {
		return false
	}
	m.textarea.SetHeight(lines)
	if value != "" && totalLines <= lines {
		// El editor anterior conservaba un YOffset obsoleto cuando
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
		Width:    chatBorderedContentWidth(w),
		Theme:    m.ctx.Styles.Theme,
		Query:    m.textarea.Value(),
	}.View()
}

func (m *ChatModel) inputBoxView(w int) string {
	s := m.ctx.Styles
	contentWidth := chatBorderedContentWidth(w)
	style := s.InputBoxFocused
	switch m.selectedAgentMode() {
	case planstate.Plan:
		style = style.BorderForeground(s.Theme.Secondary)
	case planstate.Goal:
		style = style.BorderForeground(s.Theme.Success)
	}
	box := style.Width(contentWidth).Render(m.textarea.View())
	if m.mode == ModeBash {
		box = s.Badge.Render(" BASH ") + "\n" + box
	}
	return box
}

// queuePanelView renderiza los mensajes pendientes dentro de la zona inferior
// de interacción. Al desplazarse hacia historial antiguo, esta zona completa
// sale de pantalla junto con el editor y vuelve al regresar al fondo.
func (m *ChatModel) queuePanelView(w int) string {
	if len(m.queue) == 0 {
		return ""
	}
	s := m.ctx.Styles
	boxWidth := chatBorderedContentWidth(w)
	steer, follow := 0, 0
	for _, item := range m.queue {
		if item.Mode == queueFollowUp {
			follow++
		} else {
			steer++
		}
	}
	header := fmt.Sprintf("En cola · %d pendiente(s)", len(m.queue))
	if steer > 0 || follow > 0 {
		header += fmt.Sprintf(" · dirigir %d · después %d", steer, follow)
	}
	header += "  (Alt+↑ editar · Esc aborta y restaura)"

	lines := []string{s.Accent.Render(header)}
	maxShow := 5
	for i, item := range m.queue {
		if i >= maxShow {
			lines = append(lines, s.Muted.Render(fmt.Sprintf("  … y %d más", len(m.queue)-maxShow)))
			break
		}
		kind := "dirigir"
		if item.Mode == queueFollowUp {
			kind = "después"
		}
		prefix := fmt.Sprintf("  %d. [%s] ", i+1, kind)
		// Ancho útil para el contenido tras el prefijo, dejando margen para
		// el borde y padding del panel.
		avail := boxWidth - len(prefix) - 4
		if avail < 10 {
			avail = 10
		}
		lines = append(lines, s.Muted.Render(prefix)+truncateOneLine(firstLine(item.Text), avail))
	}
	body := strings.Join(lines, "\n")
	panel := tuistyle.NewStyle().
		Width(boxWidth).
		Border(tuistyle.RoundedBorder()).
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

// pinnedActivityView renderiza el shimmer dentro de la zona inferior de interacción.
// Esa zona sólo permanece visible cuando el transcript está al fondo; al leer
// historial se desplaza fuera junto con el input y el resto del chrome.
func (m *ChatModel) pinnedActivityView(w int) string {
	if !(m.thinking || m.working) {
		return ""
	}
	var body string
	if m.compacting {
		body = RenderCompacting(m.thinkingFrame)
	} else if m.working {
		body = RenderWorking(m.thinkingFrame)
	} else {
		body = RenderThinking(m.thinkingFrame)
	}
	boxWidth := chatPaddedContentWidth(w)
	return tuistyle.NewStyle().Width(boxWidth).Padding(0, 1).Render(body)
}

// bottomChromeParts devuelve exactamente los bloques que se dibujan debajo
// del transcript. Mantener una única fuente de verdad evita que Resize y View
// diverjan cuando aparecen/desaparecen TodoWrite, actividad, cola o paleta.
func (m *ChatModel) bottomChromeParts(w, usedTokens, maxTokens int) []string {
	// Nothing is pinned while the user is reading older transcript content.
	// The viewport immediately reclaims every footer row, so TodoWrite, activity,
	// queue, questions, editor and status behave like the tail of the document
	// instead of floating over it. Returning to the bottom restores them.
	if m.userScrolled {
		return nil
	}

	// OpenCode keeps questions in the footer instead of replacing the whole TUI.
	// While the dock is open it temporarily owns the input area, keeping the
	// transcript as large as possible on short terminals.
	if dock := m.planQuestionDockView(w); dock != "" {
		return []string{dock, RenderStatusBar(m.ctx, string(m.mode), usedTokens, maxTokens)}
	}

	parts := make([]string, 0, 8)
	// Keep the pending-question launcher first so its mouse row is deterministic
	// regardless of TodoWrite/activity/queue widgets below it.
	if launcher := m.planQuestionLauncherView(w); launcher != "" {
		parts = append(parts, launcher)
	}
	if plan := m.planWidgetView(w); plan != "" {
		parts = append(parts, plan)
	}
	if todo := m.todoWidgetView(w); todo != "" && m.effectiveAgentMode() != planstate.Plan {
		parts = append(parts, todo)
	}
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
		RenderStatusBar(m.ctx, string(m.mode), usedTokens, maxTokens),
	)
	return parts
}

func (m *ChatModel) bottomChromeView(w, usedTokens, maxTokens int) string {
	return strings.Join(m.bottomChromeParts(w, usedTokens, maxTokens), "\n")
}

func (m *ChatModel) bottomChromeHeight(w int) int {
	chrome := m.bottomChromeView(w, 0, 0)
	if chrome == "" {
		return 0
	}
	// View concatena transcript + "\n" + chrome. Ese salto sólo separa
	// ambas regiones; no crea una fila vacía adicional.
	return tuistyle.Height(chrome)
}

func (m *ChatModel) viewportHeightForFrame(w, h, usedTokens, maxTokens int) int {
	if h <= 0 {
		if m.viewport.Height > 0 {
			return m.viewport.Height
		}
		return 1
	}
	chrome := m.bottomChromeView(w, usedTokens, maxTokens)
	chromeHeight := 0
	if chrome != "" {
		chromeHeight = tuistyle.Height(chrome)
	}
	height := h - chromeHeight
	if height < 1 {
		height = 1
	}
	return height
}

// viewportForFrame corrige en render la geometría dinámica del transcript sin
// depender de que cada transición visual emita un WindowSizeMsg/Resize. Esto es
// importante porque flags como thinking/working y paneles como TodoWrite pueden
// cambiar entre dos frames manteniendo exactamente el mismo tamaño de terminal.
func (m *ChatModel) viewportForFrame(w, h, usedTokens, maxTokens int) viewport.Model {
	vp := m.viewport
	targetHeight := m.viewportHeightForFrame(w, h, usedTokens, maxTokens)
	if vp.Height == targetHeight {
		return vp
	}

	wasAtBottom := m.viewport.AtBottom()
	vp.Height = targetHeight
	if !m.userScrolled || wasAtBottom {
		vp.GotoBottom()
	} else {
		vp.SetYOffset(m.viewport.YOffset)
	}
	return vp
}

// syncViewportGeometry actualiza también el modelo persistido antes de procesar
// input/scroll. View() ya corrige el frame inmediatamente; este sync hace que
// el siguiente evento de navegación opere con la misma geometría que el usuario
// está viendo.
func (m *ChatModel) syncViewportGeometry() {
	if m == nil || m.ctx == nil || m.ctx.Width <= 0 || m.ctx.Height <= 0 {
		return
	}
	used, maxCtx := m.contextUsage()
	targetHeight := m.viewportHeightForFrame(m.ctx.Width, m.ctx.Height, used, maxCtx)
	if targetHeight == m.viewport.Height {
		return
	}
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.Height = targetHeight
	if !m.userScrolled || wasAtBottom {
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetYOffset(m.viewport.YOffset)
	}
}

// returnToInteractionBottom restores the editor/footer before a key that edits
// or submits text. This avoids accepting invisible input while the user is in
// full-height transcript reading mode.
func (m *ChatModel) returnToInteractionBottom() {
	if !m.userScrolled {
		return
	}
	m.userScrolled = false
	m.viewport.GotoBottom()
	m.syncViewportGeometry()
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
	seen := map[string]bool{}
	for _, row := range rows {
		seen[strings.ToLower(row.Name)] = true
	}
	for _, s := range m.loadSkills() {
		if !s.UserInvocable || seen[strings.ToLower(s.Name)] {
			continue
		}
		if q != "" {
			if _, ok := subsequenceMatch(s.Name, q); !ok {
				if _, ok2 := subsequenceMatch("skills:"+s.Name, q); !ok2 {
					continue
				}
			}
		}
		name := s.Name
		kind := "skill"
		if s.LegacyCommand {
			kind = "comando Claude"
		}
		desc := kind + " · " + s.Description
		skillName := s.Name
		rows = append(rows, SlashCommand{
			Name:        name,
			Usage:       s.ArgumentHint,
			Description: desc,
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				return chat.invokeSkill(skillName, args)
			},
		})
	}
	return rows
}

func (m *ChatModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	// Los componentes inferiores son dinámicos (TodoWrite, actividad, cola,
	// paleta e input autoajustable). Sólo los eventos interactivos necesitan que
	// el viewport persistido coincida con el frame visible antes de navegar. Los
	// deltas SSE y timers no usan esa geometría; recalcular todo el chrome por
	// token era trabajo innecesario y podía frenar el stream.
	switch msg.(type) {
	case uikit.KeyMsg, uikit.MouseMsg:
		m.syncViewportGeometry()
	}
	switch v := msg.(type) {
	case uikit.WindowSizeMsg:
		m.Resize(v.Width, v.Height)
		return m, nil

	case thinkingTickMsg:
		if !m.thinking && !m.working {
			return m, nil
		}
		m.thinkingFrame = v.frame
		// El shimmer vive fuera del viewport. el runtime vuelve a ejecutar View
		// tras este mensaje, así que reconstruir el transcript aquí sólo hacía
		// trabajo O(historial) unas 11 veces por segundo sin cambiar su contenido.
		return m, thinkingTick(v.frame)

	case transcriptRefreshTickMsg:
		if !m.transcriptRefreshPending {
			return m, nil
		}
		autoBottom := m.transcriptRefreshAutoBottom
		m.refreshTranscript(autoBottom)
		return m, nil

	case livePersistDoneMsg:
		_ = v.err // el guardado en segundo plano nunca debe romper el turno
		m.livePersistPending = false
		// Los errores de persistencia de fondo no deben romper la conversación;
		// la siguiente frontera estable volverá a intentar el guardado completo.
		if m.livePersistDirty && m.activeTurnID != 0 {
			return m, m.requestLivePersist()
		}
		return m, nil

	case livePersistTickMsg:
		m.livePersistTimer = false
		if !m.livePersistDirty || m.activeTurnID == 0 {
			return m, nil
		}
		return m, m.requestLivePersist()

	case pasteEnterDecisionMsg:
		if v.seq != m.pendingEnterSeq || !m.pendingEnter {
			return m, nil
		}
		m.pendingEnter = false
		// Si Enter pertenecía al selector de comandos, mantenemos el
		// comportamiento histórico: ejecutar la fila resaltada. La breve espera
		// sólo sirve para dar oportunidad a un paste multilínea de continuar.
		if m.paletteOpen && len(m.paletteRows) > 0 {
			c := m.paletteRows[m.paletteIdx]
			m.textarea.Reset()
			m.paletteOpen = false
			m.syncInputHeight()
			return m, c.Run(m.ctx, m, "")
		}
		val := strings.TrimSpace(m.textarea.Value())
		if val == "" {
			return m, nil
		}
		return m.submit(val)

	case pasteFallbackIdleMsg:
		if v.seq == m.pasteFallbackSeq {
			m.pasteFallbackActive = false
			m.pasteAwaitingLF = false
		}
		return m, nil

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

	case mcpReadyMsg:
		m.mcpLoading = false
		if m.mcpRuntime != nil && m.mcpRuntime != v.runtime {
			_ = m.mcpRuntime.Close()
		}
		m.mcpRuntime = v.runtime
		m.mcpSignature = v.signature
		if text := formatMCPErrors(v.errors); text != "" {
			m.AddSystem("MCP: algunos servidores no pudieron conectarse: " + text)
		}
		m.invalidateContextUsage()
		return m, nil

	case agentEventBatchMsg:
		terminalBackground := false
		for _, envelope := range v.events {
			if !m.applyAgentEventEnvelope(envelope) {
				continue
			}
			event := envelope.event
			terminalBackground = terminalBackground || (event.Background && subagents.IsTerminalEvent(event.Kind))
		}
		refreshCmd := m.refreshTranscriptStreaming(!m.userScrolled)
		var liveCmd uikit.Cmd
		if terminalBackground && m.activeTurnID == 0 {
			// Detached workers can finish while the main chat is idle. Persist the
			// terminal panel now so a process restart can recover and deliver its
			// completion notification exactly once on the next user request.
			m.persist()
		} else {
			liveCmd = m.requestLivePersist()
		}
		if v.done {
			return m, uikit.Batch(refreshCmd, liveCmd)
		}
		return m, uikit.Batch(refreshCmd, liveCmd, agentEventPump(v.ch))

	case agentEventStreamDoneMsg:
		return m, nil

	case manualAgentResultMsg:
		if v.turnID == 0 || v.turnID != m.activeTurnID {
			return m, nil
		}
		m.streaming = false
		m.thinking = false
		m.working = false
		if v.err != nil {
			if !errors.Is(v.err, context.Canceled) {
				m.AddError("Subagente " + v.agent + ": " + v.err.Error())
			}
			m.endTurn()
			m.persist()
			m.refreshTranscript(true)
			return m, uikit.Batch(m.chatMouseModeCmd(), m.drainFollowUp())
		}
		content := strings.TrimSpace(v.text)
		if content == "" {
			content = "Subagente completado sin respuesta textual."
		}
		m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: content, Time: time.Now()})
		m.appendHistory(openai.Message{Role: "assistant", Content: content})
		m.endTurn()
		m.persist()
		m.refreshTranscript(true)
		return m, uikit.Batch(m.chatMouseModeCmd(), m.drainFollowUp())

	case compactionResultMsg:
		return m, m.applyCompactionResult(v)

	case chatStreamMsg:
		// Todo chunk de proveedor debe pertenecer tanto al turno como al request
		// HTTP/SSE actualmente activo. Esto evita que el cierre tardío de un
		// request cancelado (preflight, Escape, retry) altere el siguiente request.
		if v.turnID == 0 || v.turnID != m.activeTurnID ||
			v.requestID == 0 || v.requestID != m.activeRequestID ||
			m.turnCtx == nil || m.turnCtx.Err() != nil {
			return m, nil
		}
		if v.retry != nil {
			var liveCmd uikit.Cmd
			if v.retry.Reset {
				m.resetProviderAttemptForRetry()
				liveCmd = m.requestLivePersist()
			}
			if v.retry.Recovered {
				m.clearNetworkNotice()
				m.thinking = true
				m.working = false
			} else {
				m.showNetworkRetry(*v.retry)
			}
			m.refreshTranscript(true)
			if v.ch != nil {
				return m, uikit.Batch(liveCmd, streamPump(v.ch, v.turnID, v.requestID))
			}
			return m, liveCmd
		}
		if v.err != nil {
			if cmd := m.recoverFromContextOverflow(v.err); cmd != nil {
				return m, cmd
			}
			m.accountGoalRequest()
			m.checkpointPartialAssistantHistory()
			m.finishThinkingPanel()
			for _, p := range m.livePanels {
				if p != nil && !p.Done {
					p.Cancel()
				}
			}
			for _, cp := range m.cmdPanels {
				if cp != nil && !cp.Done {
					cp.Cancel()
				}
			}
			m.streaming = false
			m.thinking = false
			m.working = false
			m.assistantActive = -1
			m.endTurn()
			m.AddError("Error del proveedor: " + v.err.Error())
			m.persist()
			// A message typed while the failed request was active is steering for
			// the next safe boundary. An error is also a boundary: consume the
			// queue now instead of leaving the user's Enter apparently ignored.
			return m, m.drainFollowUp()
		}
		var refreshCmd uikit.Cmd
		liveDirty := false
		if len(v.superseded) > 0 {
			for _, idx := range v.superseded {
				if p := m.livePanels[idx]; p != nil && !p.Failed {
					p.MarkSuperseded()
				}
			}
			refreshCmd = m.refreshTranscriptStreaming(true)
			liveDirty = true
		}
		if v.thinking != "" {
			m.reasoningBuf.WriteString(v.thinking)
			if m.thinkingActive == nil {
				m.thinkingActive = &ThinkingPanel{Expanded: true}
				panel := ChatMessage{Kind: MsgThinking, Thinking: m.thinkingActive, Time: time.Now()}
				if m.assistantActive >= 0 && m.assistantActive < len(m.messages) {
					idx := m.assistantActive
					m.messages = append(m.messages, ChatMessage{})
					copy(m.messages[idx+1:], m.messages[idx:])
					m.messages[idx] = panel
					m.assistantActive++
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
			if cmd := m.refreshTranscriptStreaming(true); cmd != nil {
				refreshCmd = cmd
			}
			liveDirty = true
		}
		if v.thinkingDone && m.thinkingActive != nil {
			m.finishThinkingPanel()
			if cmd := m.refreshTranscriptStreaming(true); cmd != nil {
				refreshCmd = cmd
			}
			liveDirty = true
		}
		if len(v.toolCalls) > 0 {
			m.finishThinkingPanel()
			m.applyToolCalls(v.toolCalls)
			liveDirty = true
			// Creation/write-like calls are preflighted as soon as a partial tool call
			// exposes its path. Existing create_file targets, unconfirmed write_file
			// overwrites and legacy write calls cancel
			// only this provider request (not the whole turn), synthesize a compact
			// recovery result, and continue with the supported file tools. This prevents a
			// model from streaming hundreds of useless lines before the normal
			// execution-time guard can reject the call.
			if v.partial && len(v.toolCalls) == 1 {
				if cmd, intercepted := m.interceptExistingCreateCall(v.toolCalls[0]); intercepted {
					return m, cmd
				}
			}
			// Una tool call empieza a ser trabajo desde el PRIMER snapshot,
			// incluso mientras el proveedor todavía está transmitiendo sus
			// argumentos. Antes se apagaba "Pensando" al crear un panel de
			// archivo/comando, pero "Trabajando" sólo se encendía al llegar
			// el chunk final. En llamadas grandes (create_file, apply_diff, etc.)
			// eso dejaba varios segundos con ambos flags en false y el indicador
			// desaparecía exactamente mientras el panel decía "running".
			m.thinking = false
			m.working = true
			if v.partial {
				if cmd := m.refreshTranscriptStreaming(true); cmd != nil {
					refreshCmd = cmd
				}
				liveCmd := m.requestLivePersist()
				if v.ch != nil {
					return m, uikit.Batch(refreshCmd, liveCmd, streamPump(v.ch, v.turnID, v.requestID))
				}
				return m, uikit.Batch(refreshCmd, liveCmd)
			}
			m.pendingCall = append(m.pendingCall, v.toolCalls...)
		}
		if v.done {
			// Este request ya terminó. Las tools pueden tardar, pero ningún chunk
			// adicional de esta conexión debe volver a aceptarse mientras esperamos.
			m.activeRequestID = 0
			if m.requestCancel != nil {
				m.requestCancel()
				m.requestCancel = nil
			}
			m.finishThinkingPanel()
			text := m.streamBuf.String()
			reasoning := m.reasoningBuf.String()
			if m.goals != nil && m.goals.Active() {
				m.goalRequestTokens += int64(EstimateTokens([]openai.Message{{Role: "assistant", Content: text, ReasoningContent: reasoning}}))
			}
			m.accountGoalRequest()
			last := m.assistantActive
			if last >= 0 && last < len(m.messages) {
				m.messages[last].Content = text
			}
			m.streamBuf.Reset()
			m.reasoningBuf.Reset()

			if len(m.pendingCall) > 0 {
				calls := m.pendingCall
				m.pendingCall = nil
				m.appendHistory(openai.Message{
					Role: "assistant", Content: text, ReasoningContent: reasoning, ToolCalls: calls,
				})
				m.applyToolCalls(calls)
				for _, c := range calls {
					if IsFileTool(c.Function.Name) || IsCommandTool(c.Function.Name) || isTodoToolName(c.Function.Name) ||
						isPlanQuestionToolName(c.Function.Name) || isPlanExitToolName(c.Function.Name) || isAgentToolName(c.Function.Name) {
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
				m.assistantActive = -1
				m.runningCalls = append(m.runningCalls[:0], calls...)
				m.refreshTranscript(true)
				// The assistant tool_call is now protocol-complete. Checkpoint only
				// the current turn before launching the external tool; rewriting the
				// entire historical session here would reintroduce long-chat lag.
				m.forceLivePersist()
				batch := []uikit.Cmd{m.runTools(calls, text), thinkingTick(m.thinkingFrame)}

				if tick := m.maybeStartElapsedTick(); tick != nil {
					batch = append(batch, tick)
				}
				return m, uikit.Batch(batch...)
			}

			if text != "" {
				m.appendHistory(openai.Message{Role: "assistant", Content: text, ReasoningContent: reasoning})
				m.toolFallback = ""
			} else {
				// Tras una tool call, algunos modelos cierran el turno sin texto.
				// Sólo en este punto (la API ya cerró el stream) materializamos una
				// respuesta de cierre; nunca existe una burbuja vacía durante espera.
				if fallback := strings.TrimSpace(m.toolFallback); fallback != "" {
					idx := m.ensureAssistantMessage()
					m.messages[idx].Content = fallback
					m.appendHistory(openai.Message{Role: "assistant", Content: fallback})
					m.toolFallback = ""
				} else {
					idx := m.ensureAssistantMessage()
					m.messages[idx].Content = "(el modelo no devolvió contenido)"
				}
			}
			// Enter-queued steering belongs to this same agent turn. Once the
			// current assistant response has reached a safe boundary, inject one
			// queued instruction and continue before considering the turn finished.
			if m.deliverSteering() {
				m.forceLivePersist()
				m.thinking = true
				m.working = false
				m.assistantActive = -1
				return m, m.runTurn()
			}
			if m.continueGoalAtBoundary() {
				return m, m.runTurn()
			}

			m.thinking = false
			m.working = false
			m.assistantActive = -1
			// Refresh the mutable tail before dropping streaming so a long chat does
			// not rebuild the entire transcript on the completion frame.
			m.refreshTranscript(true)
			m.streaming = false
			m.endTurn()
			m.persist()
			return m, m.drainFollowUp()
		}
		if v.delta != "" {
			m.finishThinkingPanel()
			m.thinking = false
			m.streamBuf.WriteString(v.delta)
			last := m.ensureAssistantMessage()
			m.messages[last].Content = m.streamBuf.String()
			if cmd := m.refreshTranscriptStreaming(true); cmd != nil {
				refreshCmd = cmd
			}
			liveDirty = true
		}
		var liveCmd uikit.Cmd
		if liveDirty {
			liveCmd = m.requestLivePersist()
		}
		if v.ch != nil {
			return m, uikit.Batch(refreshCmd, liveCmd, streamPump(v.ch, v.turnID, v.requestID))
		}
		return m, uikit.Batch(refreshCmd, liveCmd)

	case toolResultsMsg:
		if v.turnID == 0 || v.turnID != m.activeTurnID || m.turnCtx == nil || m.turnCtx.Err() != nil {
			return m, nil
		}
		// No apagamos m.working aquí: el turno sigue activo (vamos a llamar de
		// nuevo al modelo con runTurn). Dejar working en true evita que el
		// shimmer parpadee o desaparezca entre la tool y la siguiente respuesta.

		if v.err != nil {
			m.streaming = false
			m.thinking = false
			m.working = false
			m.runningCalls = nil
			m.endTurn()
			m.AddError(v.err.Error())
			m.persist()
			return m, m.drainFollowUp()
		}
		m.runningCalls = nil
		if len(v.materialized) > 0 {
			m.activeTools = m.rememberToolsForMode(m.turnAgentMode, append(m.activeTools, v.materialized...))
			m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", m.turnAgentMode))
			m.invalidateContextUsage()
		}
		for _, callID := range v.compactCallIDs {
			m.compactRejectedCreateCall(callID)
		}
		if v.recoverEditors {
			m.switchCreateToolToEditors()
		}
		if v.recoverCreate {
			m.enableCreateTool()
		}
		m.appendHistory(v.results...)
		m.toolFallback = summarizeToolResults(v.results)
		if v.todoChanged {
			m.invalidateContextUsage()
		}

		for _, r := range v.results {
			if (isTodoToolName(r.Name) || isPlanQuestionToolName(r.Name) || isPlanExitToolName(r.Name) || isAgentToolName(r.Name)) &&
				!strings.HasPrefix(strings.TrimSpace(r.Content), "error:") {
				continue
			}
			if p := m.panelByCall[r.ToolCallID]; p != nil {
				if isCreateFileTool(p.Tool) && (strings.HasPrefix(strings.TrimSpace(r.Content), "FILE_EXISTS:") ||
					strings.HasPrefix(strings.TrimSpace(r.Content), "OVERWRITE_REQUIRED:") ||
					strings.HasPrefix(strings.TrimSpace(r.Content), "USE_CREATE_FILE:") ||
					strings.HasPrefix(strings.TrimSpace(r.Content), "WRITE_BLOCKED:")) {
					// The body was never applied; do not keep hundreds of rejected
					// lines occupying the transcript after the skip.
					p.Content = ""
				}
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
		if v.todoChanged && m.ctx.Width > 0 && m.ctx.Height > 0 {
			m.Resize(m.ctx.Width, m.ctx.Height)
		} else {
			m.refreshTranscript(true)
		}
		var todoMouseCmd uikit.Cmd
		if v.todoChanged {
			todoMouseCmd = m.chatMouseModeCmd()
		}

		// Plan questions and plan_exit deliberately END the current agent turn.
		// The user answers normally or presses Tab to select Build; we must not
		// immediately call the model again and accidentally plan/implement past
		// that human decision boundary.
		if v.planQuestion || v.planCompleted {
			m.toolFallback = ""
			m.streaming = false
			m.thinking = false
			m.working = false
			m.assistantActive = -1
			if v.planCompleted && m.plans != nil {
				if plan := strings.TrimSpace(m.plans.LatestPlan()); plan != "" {
					m.messages = append(m.messages, ChatMessage{Kind: MsgAssistant, Content: "## Plan\n\n" + plan, Time: time.Now()})
				}
			}
			m.endTurn()
			m.persist()
			if v.planQuestion {
				return m, m.openPlanQuestions()
			}
			if m.ctx.Width > 0 && m.ctx.Height > 0 {
				m.Resize(m.ctx.Width, m.ctx.Height)
			} else {
				m.refreshTranscript(true)
			}
			return m, uikit.Batch(todoMouseCmd, m.chatMouseModeCmd())
		}

		if m.goalStopsCurrentLoop() {
			m.toolFallback = ""
			m.streaming = false
			m.thinking = false
			m.working = false
			m.assistantActive = -1
			m.endTurn()
			if state := m.goalStatePointer(); state != nil {
				m.AddSystem("Goal detenido: " + goalStatusLabel(state.Status) + ".")
			}
			m.persist()
			return m, uikit.Batch(todoMouseCmd, m.chatMouseModeCmd(), m.drainFollowUp())
		}

		// Tool outputs are a stable API boundary. This is also the preferred
		// steering boundary: current calls have completed, so one Enter-queued
		// instruction can safely influence the very next model request.
		m.deliverSteering()
		m.forceLivePersist()
		return m, uikit.Batch(m.runTurn(), todoMouseCmd)

	case bashResultMsg:
		m.endRewindExternalOperation()
		m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: v.output, Time: time.Now()})
		m.refreshTranscript(true)
		return m, nil

	case uikit.MouseMsg:
		if handled, cmd := m.handlePlanQuestionMouse(v); handled {
			return m, cmd
		}
		if handled, cmd := m.handleTodoMouse(v); handled {
			return m, cmd
		}
		var cmd uikit.Cmd
		m.viewport, cmd = m.viewport.Update(v)
		m.userScrolled = !m.viewport.AtBottom()
		return m, uikit.Batch(cmd, m.chatMouseModeCmd())

	case uikit.KeyMsg:
		if handled, cmd := m.handlePlanQuestionKey(v); handled {
			return m, cmd
		}
		key := v.String()
		var pasteCmd uikit.Cmd
		// tview entrega bracketed paste como un bloque único marcado con Paste
		// activo. Insertamos el bloque completo de una vez: los saltos de línea
		// son texto, nunca eventos Enter, y un pegado grande no se "teclea" rune
		// por rune a través del loop principal.
		if v.Paste {
			m.returnToInteractionBottom()
			// El camino nativo es autoritativo. Un paste bracketed nunca debe
			// heredar el estado heurístico de una entrada anterior.
			m.resetPasteFallback()
			pasted := string(v.Runes)
			pasted = strings.ReplaceAll(pasted, "\r\n", "\n")
			pasted = strings.ReplaceAll(pasted, "\r", "\n")
			if pasted != "" {
				m.textarea.InsertString(pasted)
				m.updatePalette()
				m.syncInputHeight()
			}
			return m, nil
		}

		// Algunos hosts pierden los delimitadores bracketed-paste y entregan
		// "texto, Enter, texto". El primer Enter queda pendiente; la llegada
		// inmediata de contenido posterior confirma que era un salto pegado.
		// CRLF se coalesce: el LF que sigue al CR confirma el salto pero no
		// inserta una segunda línea vacía.
		if m.pendingEnter && isPasteContinuationKey(v) {
			pasteCmd = m.confirmPendingEnterAsPaste()
			if v.Type == uikit.KeyCtrlJ {
				// CR + LF representan un único salto de línea.
				m.pasteAwaitingLF = false
				return m, pasteCmd
			}
		}

		// Si el CR anterior ya insertó la nueva línea, consumir su LF compañero
		// sin crear una línea vacía adicional. Si llega cualquier otra cosa, el
		// terminal estaba usando CR solo y seguimos procesándola normalmente.
		if m.pasteFallbackActive && m.pasteAwaitingLF {
			if v.Type == uikit.KeyCtrlJ {
				m.pasteAwaitingLF = false
				return m, m.armPasteFallback()
			}
			m.pasteAwaitingLF = false
		}

		// Resolver Enter antes del palette y del resto de atajos. De otro modo
		// un texto pegado que empieza por "/" podría ejecutar una sugerencia en
		// cuanto llegara su primer salto de línea.
		if key == "enter" {
			m.returnToInteractionBottom()
			if m.pasteFallbackActive {
				m.textarea.InsertString("\n")
				m.pasteAwaitingLF = true
				m.updatePalette()
				m.syncInputHeight()
				return m, m.armPasteFallback()
			}
			if strings.TrimSpace(m.textarea.Value()) == "" {
				return m, nil
			}
			return m, m.deferEnterSubmit()
		}

		// Un LF inmediatamente posterior a un CR fue consumido arriba como la
		// segunda mitad de CRLF. Los LF siguientes, mientras el paste siga
		// confirmado, sí representan líneas nuevas.
		if m.pasteFallbackActive && v.Type == uikit.KeyCtrlJ {
			m.textarea.InsertString("\n")
			m.updatePalette()
			m.syncInputHeight()
			return m, m.armPasteFallback()
		}

		// Teclas de scroll: siempre van al viewport, incluso durante streaming
		// o ejecución de herramientas. Permiten leer el historial mientras
		// Lilith trabaja.
		if isScrollKey(key) {
			var cmd uikit.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.userScrolled = !m.viewport.AtBottom()
			return m, uikit.Batch(cmd, m.chatMouseModeCmd())
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
			}
		}
		// Tab cycles the primary agents for the NEXT user message. A running turn
		// keeps its Build/Plan/Goal snapshot; Shift+Tab traverses in reverse.
		if key == "tab" || key == "shift+tab" {
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			m.cycleAgentMode(delta)
			return m, m.chatMouseModeCmd()
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
			m.invalidateTranscriptCache()
			m.refreshTranscript(true)
			return m, nil
		case "ctrl+t":
			m.returnToInteractionBottom()
			if m.toggleTodoExpanded() {
				return m, m.chatMouseModeCmd()
			}
			return m, nil
		case "ctrl+r":
			// Toggle del último panel de "pensamiento".
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Kind == MsgThinking && m.messages[i].Thinking != nil {
					m.messages[i].Thinking.Expanded = !m.messages[i].Thinking.Expanded
					m.invalidateTranscriptCache()
					m.refreshTranscript(true)
					break
				}
			}
			return m, nil
		case "ctrl+g":
			// Toggle del subagente más reciente. Los paneles siguen formando
			// parte del transcript y nunca quedan anclados al viewport.
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Kind == MsgAgent && m.messages[i].Agent != nil {
					m.messages[i].Agent.Expanded = !m.messages[i].Agent.Expanded
					m.invalidateTranscriptCache()
					m.refreshTranscript(true)
					break
				}
			}
			return m, nil
		case "esc":
			// Pi usa Escape como interrupción. La cola no se pierde: después de
			// abortar vuelve al editor para poder corregirla o reenviarla.
			m.pendingEnter = false
			m.pendingEnterSeq++
			if m.compacting && m.activeTurnID == 0 {
				m.cancelManualCompaction()
			} else if m.streaming && m.activeTurnID != 0 {
				m.cancelTurn()
			}
			restored := m.restoreQueuedToEditor()
			if restored > 0 {
				m.AddSystem(fmt.Sprintf("%d mensaje(s) de la cola devueltos al editor.", restored))
			}
			return m, nil
		case "alt+up":
			// Recuperar la cola no toca el turno activo; sirve para editar un
			// steering/follow-up antes de que llegue a su frontera de entrega.
			m.pendingEnter = false
			m.pendingEnterSeq++
			m.restoreQueuedToEditor()
			return m, nil
		case "ctrl+c":
			// Ctrl+C limpia únicamente el borrador del editor. El turno activo y
			// cualquier steering/follow-up ya encolado continúan sin cambios; Esc
			// sigue siendo la interrupción explícita de la tarea.
			m.returnToInteractionBottom()
			m.resetPasteFallback()
			if m.textarea.Value() == "" {
				return m, nil
			}
			m.textarea.Reset()
			m.updatePalette()
			m.syncInputHeight()
			return m, nil
		case "ctrl+z":
			// No suspender Lilith desde el runtime interactivo. /exit conserva la
			// salida explícita y evita dejar la terminal en un estado inconsistente.
			return m, nil
		case "alt+enter":
			m.returnToInteractionBottom()
			// Follow-up explícito: durante una tarea espera a que el agente haya
			// terminado todo el trabajo; en reposo equivale a enviar normalmente.
			m.pendingEnter = false
			m.pendingEnterSeq++
			val := strings.TrimSpace(m.textarea.Value())
			if val == "" {
				return m, nil
			}
			if m.streaming {
				m.resetPasteFallback()
				m.textarea.Reset()
				m.paletteOpen = false
				m.syncInputHeight()
				m.enqueue(val, queueFollowUp)
				return m, nil
			}
			return m.submit(val)
		case "shift+enter", "ctrl+enter":
			m.returnToInteractionBottom()
			// Nueva línea explícita dentro del textarea. No entra en el detector
			// de paste porque el usuario indicó de forma inequívoca que quiere
			// una línea nueva.
			m.pendingEnter = false
			m.pendingEnterSeq++
			m.textarea.InsertString("\n")
			m.syncInputHeight()
			return m, nil
		}

		// Cualquier entrada de texto vuelve primero al final del documento; el
		// editor nunca recibe pulsaciones invisibles mientras se lee historial.
		m.returnToInteractionBottom()

		// Para teclas normales dejamos que el editor interno actualice el textarea y
		// conservamos, si aplica, el timer del fallback de paste. Retornar aquí
		// evita que el mismo KeyMsg se procese dos veces en el bloque genérico.
		prev := m.textarea.Value()
		var cmd uikit.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		if m.textarea.Value() != prev {
			m.updatePalette()
			m.syncInputHeight()
		}
		if m.pasteFallbackActive && isPasteContinuationKey(v) {
			pasteCmd = m.armPasteFallback()
		}
		return m, uikit.Batch(cmd, pasteCmd)
	}

	var cmds []uikit.Cmd
	if m.planQuestion.editing {
		var qcmd uikit.Cmd
		m.planQuestion.input, qcmd = m.planQuestion.input.Update(msg)
		cmds = append(cmds, qcmd)
	}
	prev := m.textarea.Value()
	var cmd uikit.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)
	if m.textarea.Value() != prev {
		m.updatePalette()
		m.syncInputHeight()
	}
	if _, isKey := msg.(uikit.KeyMsg); !isKey {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
		// El viewport puede haberse desplazado por rueda del mouse. Ajusta
		// el flag para que el auto-scroll respete la posición del usuario.
		if _, isMouse := msg.(uikit.MouseMsg); isMouse {
			m.userScrolled = !m.viewport.AtBottom()
			cmds = append(cmds, m.chatMouseModeCmd())
		}
	}
	return m, uikit.Batch(cmds...)
}

// Queue helpers implementan dos clases de mensajes como Pi: steering para la
// siguiente frontera segura del agente y follow-up para después del turno.
func (m *ChatModel) enqueue(text string, mode queueMode) {
	m.queue = append(m.queue, queuedMessage{Text: text, Mode: mode})
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
}

func (m *ChatModel) takeQueued(mode queueMode) (queuedMessage, bool) {
	for i, item := range m.queue {
		if item.Mode != mode {
			continue
		}
		m.queue = append(m.queue[:i], m.queue[i+1:]...)
		if m.ctx.Width > 0 && m.ctx.Height > 0 {
			m.Resize(m.ctx.Width, m.ctx.Height)
		}
		return item, true
	}
	return queuedMessage{}, false
}

func (m *ChatModel) extendActiveToolsForPrompt(text string) {
	mode := m.effectiveAgentMode()
	for _, name := range m.selectToolsForPrompt(text, mode) {
		m.activeTools = appendUniqueTool(m.activeTools, name)
	}
	m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", mode))
	sort.Strings(m.activeTools)
	m.invalidateContextUsage()
}

// deliverSteering consumes exactly one Enter-queued message and injects it at
// the next safe agent boundary. This mirrors pi's default one-at-a-time
// steering mode: current tool calls finish, then the instruction is visible to
// the next model request without starting a parallel turn.
func (m *ChatModel) deliverSteering() bool {
	item, ok := m.takeQueued(queueSteer)
	if !ok {
		return false
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: item.Text, Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: item.Text})
	m.extendActiveToolsForPrompt(item.Text)
	m.toolFallback = ""
	m.refreshTranscript(true)
	return true
}

// drainFollowUp starts one Alt+Enter follow-up only after the active agent work
// has fully stopped. A stray steering item is preferred defensively so ordering
// remains useful even if it was queued on the final completion frame.
func (m *ChatModel) drainFollowUp() uikit.Cmd {
	if m.streaming || len(m.queue) == 0 {
		return nil
	}
	item, ok := m.takeQueued(queueSteer)
	if !ok {
		item, ok = m.takeQueued(queueFollowUp)
	}
	if !ok {
		return nil
	}
	_, cmd := m.submit(item.Text)
	return cmd
}

// restoreQueuedToEditor is the non-destructive queue escape hatch used by Pi:
// queued messages return to the editor so the user can edit/copy/re-submit
// them. The active task is not touched unless the caller explicitly aborts it.
func (m *ChatModel) restoreQueuedToEditor() int {
	if len(m.queue) == 0 {
		return 0
	}
	parts := make([]string, 0, len(m.queue)+1)
	for _, item := range m.queue {
		if strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	if current := strings.TrimSpace(m.textarea.Value()); current != "" {
		parts = append(parts, m.textarea.Value())
	}
	count := len(m.queue)
	m.queue = nil
	m.textarea.SetValue(strings.Join(parts, "\n"))
	m.textarea.CursorEnd()
	m.updatePalette()
	m.syncInputHeight()
	if m.ctx.Width > 0 && m.ctx.Height > 0 {
		m.Resize(m.ctx.Width, m.ctx.Height)
	}
	return count
}

func (m *ChatModel) submit(val string) (uikit.Model, uikit.Cmd) {
	m.resetPasteFallback()
	m.textarea.Reset()
	m.paletteOpen = false
	m.syncInputHeight()

	// /exit es una orden de proceso, no un mensaje para el agente. Debe cerrar
	// inmediatamente incluso si hay streaming, sin quedar atrapado en steering.
	if strings.EqualFold(strings.TrimSpace(val), "/exit") {
		if m.activeTurnID != 0 {
			m.cancelTurn()
		}
		if m.finalizeRunningAgentPanels("Cancelado al cerrar Lilith.") {
			m.persist()
		}
		m.runSessionHook("SessionEnd")
		if m.sessionCancel != nil {
			m.sessionCancel()
		}
		if m.mcpRuntime != nil {
			_ = m.mcpRuntime.Close()
		}
		return m, uikit.Quit
	}

	// Codex exposes /goal while a task is running. Treat it as durable state,
	// never as queued chat text, so updating the objective cannot be delayed
	// behind an unrelated tool batch.
	trimmedVal := strings.TrimSpace(val)
	if strings.EqualFold(trimmedVal, "/goal") || strings.HasPrefix(strings.ToLower(trimmedVal), "/goal ") {
		args := strings.TrimSpace(trimmedVal[len("/goal"):])
		if m.activeTurnID == 0 && goalCommandNeedsCheckpoint(args) {
			m.beginRewindPoint(trimmedVal)
		}
		return m, m.runGoalCommand(args)
	}

	// Goal is a first-class primary agent. Plain text entered while selected is
	// treated exactly like `/goal <objetivo>` while still appearing as a normal
	// user message in the transcript. During a running task this safely steers
	// the active parent instead of opening a parallel turn.
	if m.selectedAgentMode() == planstate.Goal && trimmedVal != "" &&
		!strings.HasPrefix(trimmedVal, "/") && !strings.HasPrefix(trimmedVal, "!") && !strings.HasPrefix(trimmedVal, "@") {
		m.beginRewindPoint(val)
		m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: val, Time: time.Now()})
		m.refreshTranscript(true)
		return m, m.runGoalCommand(trimmedVal)
	}

	// Enter durante una tarea es steering: no abre un turno paralelo. Se
	// entrega en la siguiente frontera segura (después de las tools actuales)
	// para que pueda reorientar el trabajo en curso, igual que en Pi.
	if m.streaming {
		m.enqueue(val, queueSteer)
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
		if m.skillsEnabled() {
			if sk := skills.Find(m.loadSkills(), strings.TrimPrefix(name, "/")); sk != nil && sk.UserInvocable {
				return m, m.invokeSkill(sk.Name, args)
			}
		}
		m.AddError("Comando no reconocido: " + name)
		return m, nil
	}
	if strings.HasPrefix(val, "!") {
		cmd := strings.TrimSpace(val[1:])
		if cmd == "" {
			return m, nil
		}
		if m.selectedAgentMode() == planstate.Plan && !planstate.IsSafeCommand(cmd) {
			m.AddError("Plan mode bloquea este comando: cambia a Build con Tab para ejecutar comandos que puedan modificar el sistema.")
			return m, nil
		}
		m.beginRewindPoint("!" + cmd)
		m.messages = append(m.messages, ChatMessage{Kind: MsgTool, Content: "$ " + cmd, Time: time.Now()})
		m.refreshTranscript(true)
		return m, m.runBash(cmd)
	}

	// OpenCode-style direct subagent invocation. Unknown @mentions remain normal
	// chat text so users can still discuss handles/names without special syntax.
	if strings.HasPrefix(val, "@") {
		parts := strings.Fields(val)
		if len(parts) > 0 {
			name := strings.TrimPrefix(parts[0], "@")
			if agents.Find(m.loadAgents(), name) != nil {
				prompt := strings.TrimSpace(strings.TrimPrefix(val, parts[0]))
				return m.invokeAgentDirect(name, prompt, val)
			}
		}
	}

	// UserPromptSubmit hooks are external commands and may mutate files before
	// the provider sees the turn. Establish the boundary first and capture the
	// workspace whenever hooks are enabled, so their side effects are rewindable
	// too. A blocked hook keeps the checkpoint because it may already have run.
	m.beginRewindPoint(val)
	if m.hookRunner().Count() > 0 {
		_ = m.ensureActiveRewindWorkspace()
	}
	modelPrompt, hookErr := m.runUserPromptHooks(val)
	if hookErr != nil {
		m.AddError(hookErr.Error())
		return m, nil
	}
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: val, Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: modelPrompt})
	// Build/Plan/Goal are primary agents. Capture the currently
	// selected agent for lazy tool selection; beginTurn snapshots the same mode
	// so a Tab press during execution only affects the NEXT user turn.
	selectedMode := m.selectedAgentMode()
	m.activeTools = m.selectToolsForPrompt(val, selectedMode)
	m.toolFallback = ""
	if err := m.beginTurn(); err != nil {
		m.AddError(err.Error())
		return m, nil
	}
	// A real user turn supersedes any unanswered Plan request. The persisted
	// manager cleared it in beginTurn; reset only the local dock presentation.
	m.planQuestion.resetPresentation()
	if m.turnAgentMode != planstate.Plan {
		m.cleanupCompletedTodos()
	}
	m.persistTurnStart()
	return m, uikit.Batch(m.runTurn(), m.chatMouseModeCmd())
}

// runTurn envía el historial actual al modelo con los esquemas de herramientas
// activos y arranca el streaming.
func (m *ChatModel) runTurn() uikit.Cmd {
	turnID := m.activeTurnID
	if turnID == 0 || m.turnCtx == nil || m.turnCtx.Err() != nil {
		return nil
	}
	// A model may create/resume a durable goal through a tool after the parent
	// turn already started. Bind it before the next provider request so a later
	// blocked/complete/clear transition stops only this managed loop.
	if m.goals != nil && m.goals.Active() {
		m.turnGoalManaged = true
	}
	provider := m.ctx.Providers.FindProvider(m.turnProvider)
	if provider == nil {
		m.endTurn()
		m.streaming = false
		m.thinking = false
		m.working = false
		m.AddError("No hay un proveedor activo. Usa /login o /providers.")
		return nil
	}
	// Any completion produced by a detached worker belongs to this request and
	// must participate in the threshold calculation. Dynamic tools are filtered
	// first for the same reason: prepareRequestMessages must estimate the exact
	// mode/prompt state that the provider will receive.
	m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", m.turnAgentMode))
	m.deliverBackgroundAgentMessages()
	if plan, ok := m.shouldAutoCompact(); ok {
		return m.startAutoCompaction(plan)
	}
	m.livePanels = nil
	m.panelByCall = nil
	m.cmdPanels = nil
	m.cmdByCall = nil
	m.thinkingActive = nil
	m.assistantActive = -1
	m.reasoningBuf.Reset()
	m.requestMessageStart = len(m.messages)
	m.networkNoticeIndex = -1
	m.streaming = true
	m.thinking = true
	m.working = false

	m.thinkingFrame = 0
	m.streamBuf.Reset()
	// Un turno nuevo siempre vuelve a seguir el fondo. Si el usuario vuelve a
	// desplazarse hacia arriba durante la ejecución, userScrolled se activará
	// otra vez y los refrescos respetarán esa posición.
	m.userScrolled = false
	m.refreshTranscript(true)

	msgs := m.prepareRequestMessages(m.turnAgentMode)
	schemas := m.requestToolSchemas(m.turnAgentMode)
	if m.goals != nil && m.goals.Active() {
		m.goalRequestTokens = int64(compactctx.EstimateRequestTokens(msgs, schemas))
	} else {
		m.goalRequestTokens = 0
	}

	req := openai.Request{
		Provider:        *provider,
		Model:           m.turnModel,
		Messages:        msgs,
		Stream:          true,
		Tools:           schemas,
		ReasoningEffort: m.turnReasoningEffort,
	}
	if m.requestCancel != nil {
		m.requestCancel()
	}
	m.requestSeq++
	requestID := m.requestSeq
	m.activeRequestID = requestID
	requestCtx, requestCancel := context.WithCancel(m.turnCtx)
	m.requestCancel = requestCancel
	ch := m.ctx.Client.Stream(requestCtx, req)
	batch := []uikit.Cmd{streamPump(ch, turnID, requestID), thinkingTick(0)}
	if tick := m.maybeStartElapsedTick(); tick != nil {
		batch = append(batch, tick)
	}
	return uikit.Batch(batch...)
}

// contextUsage devuelve los tokens estimados del turno actual y la ventana
// máxima del modelo activo (resuelta contra el catálogo de modelos).
func (m *ChatModel) contextUsage() (int, int) {
	active := m.ctx.Providers.Active()
	if active.ModelID == "" {
		m.contextUsedCache = 0
		m.contextMaxCache = 0
		m.contextCacheProvider = active.ProviderID
		m.contextCacheModel = active.ModelID
		m.contextCacheToolSig = activeToolsSignature(m.activeTools)
		m.contextCacheDirty = false
		return 0, 0
	}
	toolSig := activeToolsSignature(m.activeTools)
	if !m.contextCacheDirty &&
		m.contextCacheProvider == active.ProviderID &&
		m.contextCacheModel == active.ModelID &&
		m.contextCacheToolSig == toolSig {
		return m.contextUsedCache, m.contextMaxCache
	}
	mode := m.effectiveAgentMode()
	msgs := m.prepareRequestMessages(mode)
	used := compactctx.EstimateRequestTokens(msgs, m.requestToolSchemas(mode))
	maxCtx := 0
	if provider := m.ctx.Providers.FindProvider(active.ProviderID); provider != nil {
		maxCtx = provider.ContextWindow(active.ModelID)
	}
	m.contextUsedCache = used
	m.contextMaxCache = maxCtx
	m.contextCacheProvider = active.ProviderID
	m.contextCacheModel = active.ModelID
	m.contextCacheToolSig = toolSig
	m.contextCacheDirty = false
	return used, maxCtx
}

// runTools ejecuta el lote de llamadas y devuelve los mensajes `tool`.
func (m *ChatModel) runTools(calls []openai.ToolCall, assistantText string) uikit.Cmd {
	turnID := m.activeTurnID
	runCtx := m.turnCtx
	if m.turnAgentMode == planstate.Plan {
		hasExit := false
		for _, call := range calls {
			if isPlanExitToolName(call.Function.Name) {
				hasExit = true
				break
			}
		}
		if hasExit && (len(calls) != 1 || strings.TrimSpace(assistantText) != "") {
			return func() uikit.Msg {
				results := make([]openai.Message, 0, len(calls))
				for _, call := range calls {
					results = append(results, toolMessage(call, "error: plan_exit debe ser la única acción final, sin texto ni otras herramientas en la misma respuesta."))
				}
				return toolResultsMsg{turnID: turnID, results: results}
			}
		}
	}

	root, _ := os.Getwd()
	var skillCatalog []skills.Skill
	if m.skillsEnabled() {
		skillCatalog = m.loadSkills()
	}
	eventSink := m.agentEventSink()
	env := m.toolEnvWithAgentEvents(root, m.turnAgentMode, eventSink)
	env.Skills = skillCatalog
	startTodoRevision := m.todoRevision()
	execCmd := func() uikit.Msg {
		results := make([]openai.Message, 0, len(calls))
		materialized := make([]string, 0, 4)
		env.Materialize = func(names []string) {
			for _, name := range names {
				materialized = appendUniqueTool(materialized, name)
			}
		}
		if len(calls) > 1 && allAgentCalls(calls) {
			results := make([]openai.Message, len(calls))
			var wg sync.WaitGroup
			for i, call := range calls {
				i, call := i, call
				wg.Add(1)
				go func() {
					defer wg.Done()
					if runCtx == nil || runCtx.Err() != nil {
						results[i] = toolMessage(call, "error: canceled")
						return
					}
					args := map[string]any{}
					if strings.TrimSpace(call.Function.Arguments) != "" {
						if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
							results[i] = toolMessage(call, "error: argumentos JSON inválidos: "+err.Error())
							return
						}
					}
					out, err := tools.Execute(runCtx, call.Function.Name, args, env)
					if err != nil {
						out = "error: " + err.Error()
					}
					results[i] = toolMessage(call, out)
				}()
			}
			wg.Wait()
			if runCtx == nil || runCtx.Err() != nil {
				return toolResultsMsg{turnID: turnID, err: context.Canceled}
			}
			return toolResultsMsg{turnID: turnID, results: results}
		}

		compactCallIDs := make([]string, 0, 1)
		recoverEditors := false
		recoverCreate := false
		planQuestion := false
		planCompleted := false
		for _, c := range calls {
			if runCtx == nil || runCtx.Err() != nil {
				return toolResultsMsg{turnID: turnID, err: context.Canceled}
			}
			args := map[string]any{}
			if strings.TrimSpace(c.Function.Arguments) != "" {
				if err := json.Unmarshal([]byte(c.Function.Arguments), &args); err != nil {
					results = append(results, toolMessage(c, "error: argumentos JSON inválidos: "+err.Error()))
					continue
				}
			}
			out, err := tools.Execute(runCtx, c.Function.Name, args, env)
			if err != nil {
				out = "error: " + err.Error()
			} else if isPlanQuestionToolName(c.Function.Name) {
				planQuestion = true
			} else if isPlanExitToolName(c.Function.Name) {
				planCompleted = true
			} else if isCreateFileTool(c.Function.Name) &&
				(strings.HasPrefix(out, "FILE_EXISTS:") || strings.HasPrefix(out, "OVERWRITE_REQUIRED:") || strings.HasPrefix(out, "USE_CREATE_FILE:") || strings.HasPrefix(out, "WRITE_BLOCKED:")) {
				// The file was never written. Keep the visible panel, but compact the
				// rejected full-file payload before the next API request so thousands
				// of useless generated tokens are not re-sent on every continuation.
				compactCallIDs = append(compactCallIDs, c.ID)
				switch {
				case strings.HasPrefix(out, "OVERWRITE_REQUIRED:"):
					// Keep write_file active so the model can explicitly confirm
					// overwrite=true or choose a targeted editor.
				case strings.HasPrefix(out, "USE_CREATE_FILE:"):
					recoverCreate = true
				case strings.HasPrefix(out, "WRITE_BLOCKED:"):
					recoverEditors = true
					recoverCreate = true
				default:
					recoverEditors = true
				}
			}
			if runCtx == nil || runCtx.Err() != nil {
				return toolResultsMsg{turnID: turnID, err: context.Canceled}
			}
			results = append(results, toolMessage(c, out))
		}
		return toolResultsMsg{
			turnID: turnID, results: results, materialized: materialized, compactCallIDs: compactCallIDs,
			recoverEditors: recoverEditors, recoverCreate: recoverCreate,
			todoChanged:   m.todoRevision() != startTodoRevision,
			planQuestion:  planQuestion,
			planCompleted: planCompleted,
		}
	}
	return execCmd
}

func isAgentToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "agent", "task":
		return true
	default:
		return false
	}
}

func allAgentCalls(calls []openai.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !isAgentToolName(call.Function.Name) {
			return false
		}
	}
	return true
}

func preflightStreamingCreateCall(root string, call openai.ToolCall) (openai.ToolCall, string, bool) {
	name := call.Function.Name
	if !isCreateFileTool(name) || strings.TrimSpace(call.ID) == "" {
		return call, "", false
	}

	// `write` is an unsupported ambiguous legacy alias. Explicit write_file and
	// append_file are native tools and must not be intercepted as hallucinations.
	if name == "write" {
		path, _ := completeJSONString(call.Function.Arguments, "path")
		result, err := tools.InterceptLegacyWrite(root, name, path)
		if err != nil {
			return call, "", false
		}
		compactArgs, err := json.Marshal(map[string]any{"path": path, "content": ""})
		if err != nil {
			return call, "", false
		}
		call.Function.Arguments = string(compactArgs)
		return call, result, true
	}

	// append_file is safe for both existing and missing targets and therefore
	// has no streaming preflight rejection.
	if name == "append_file" {
		return call, "", false
	}

	path, ok := completeJSONString(call.Function.Arguments, "path")
	path = strings.TrimSpace(path)
	if !ok || path == "" {
		return call, "", false
	}

	if name == "write_file" {
		overwrite, overwriteComplete := completeJSONBool(call.Function.Arguments, "overwrite")
		if !overwriteComplete {
			// The schema advertises overwrite before content. If content has already
			// started without it, treat omission as false and stop the payload early.
			if _, contentStarted := partialJSONString(call.Function.Arguments, "content"); !contentStarted {
				return call, "", false
			}
			overwrite = false
		}
		result, blocked, err := tools.PreflightWriteFile(root, path, overwrite)
		if err != nil || !blocked {
			return call, "", false
		}
		compactArgs, err := json.Marshal(map[string]any{"path": path, "overwrite": overwrite, "content": ""})
		if err != nil {
			return call, "", false
		}
		call.Function.Arguments = string(compactArgs)
		return call, result, true
	}

	// create_file remains valid only for a genuinely new path.
	result, exists, err := tools.PreflightCreateFile(root, path)
	if err != nil || !exists {
		return call, "", false
	}
	compactArgs, err := json.Marshal(map[string]any{"path": path, "content": ""})
	if err != nil {
		return call, "", false
	}
	call.Function.Arguments = string(compactArgs)
	return call, result, true
}

func (m *ChatModel) interceptExistingCreateCall(call openai.ToolCall) (uikit.Cmd, bool) {
	root, err := os.Getwd()
	if err != nil {
		return nil, false
	}
	call, result, intercepted := preflightStreamingCreateCall(root, call)
	if !intercepted {
		return nil, false
	}

	// Stop only the current HTTP/SSE request. Invalidate its request ID first so
	// the inevitable context.Canceled / EOF event from that old stream cannot be
	// mistaken for the replacement request we are about to start in this turn.
	m.activeRequestID = 0
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}

	m.finishThinkingPanel()
	text := m.streamBuf.String()
	reasoning := m.reasoningBuf.String()
	if m.assistantActive >= 0 && m.assistantActive < len(m.messages) {
		m.messages[m.assistantActive].Content = text
	}
	m.streamBuf.Reset()
	m.reasoningBuf.Reset()
	m.pendingCall = nil
	m.runningCalls = nil
	m.assistantActive = -1

	// The assistant/tool pair remains protocol-correct even though we cut the
	// provider stream early. The rejected payload is represented compactly and
	// never pollutes subsequent context.
	m.appendHistory(openai.Message{
		Role: "assistant", Content: text, ReasoningContent: reasoning, ToolCalls: []openai.ToolCall{call},
	})
	m.appendHistory(toolMessage(call, result))
	if p := m.panelByCall[call.ID]; p != nil {
		p.Content = ""
		p.Finish(result)
	}

	switch {
	case strings.HasPrefix(result, "OVERWRITE_REQUIRED:"):
		// write_file stays active; the next model call must make the explicit
		// overwrite decision or choose str_replace/apply_diff.
	case strings.HasPrefix(result, "USE_CREATE_FILE:"):
		m.enableCreateTool()
	case strings.HasPrefix(result, "WRITE_BLOCKED:"):
		m.switchCreateToolToEditors()
		m.enableCreateTool()
	default:
		m.switchCreateToolToEditors()
	}
	m.toolFallback = result
	m.thinking = false
	m.working = true
	m.refreshTranscript(true)
	m.forceLivePersist()
	return m.runTurn(), true
}

func (m *ChatModel) switchCreateToolToEditors() {
	seen := map[string]bool{}
	next := make([]string, 0, len(m.activeTools)+3)
	for _, name := range m.activeTools {
		if isCreateOnlyFileTool(name) || name == "write" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			next = append(next, name)
		}
	}
	for _, name := range []string{"read_files", "str_replace", "apply_diff"} {
		if seen[name] {
			continue
		}
		if _, ok := tools.Get(name); !ok {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	m.activeTools = next
	// Cache is intentionally additive across turns for prompt-cache stability.
	// The current turn may temporarily hide create_file after a policy redirect,
	// but newly materialized editor tools should remain known on later turns.
	m.rememberToolsForMode(m.turnAgentMode, next)
}

func appendUniqueTool(names []string, name string) []string {
	for _, current := range names {
		if current == name {
			return names
		}
	}
	return append(names, name)
}

func (m *ChatModel) enableCreateTool() {
	for _, name := range m.activeTools {
		if name == "create_file" {
			return
		}
	}
	if _, ok := tools.Get("create_file"); ok {
		m.activeTools = append(m.activeTools, "create_file")
		sort.Strings(m.activeTools)
		m.rememberToolsForMode(m.turnAgentMode, []string{"create_file"})
	}
}

func (m *ChatModel) compactRejectedCreateCall(callID string) {
	for i := len(m.history) - 1; i >= 0; i-- {
		for j := range m.history[i].ToolCalls {
			call := &m.history[i].ToolCalls[j]
			if call.ID != callID || !isCreateFileTool(call.Function.Name) {
				continue
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				return
			}
			path, _ := args["path"].(string)
			compact, err := json.Marshal(map[string]any{"path": path, "content": ""})
			if err != nil {
				return
			}
			call.Function.Arguments = string(compact)
			m.invalidateContextUsage()
			return
		}
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
func (m *ChatModel) runBash(command string) uikit.Cmd {
	root, _ := os.Getwd()
	m.beginRewindExternalOperation()
	return func() uikit.Msg {
		warning := ""
		if err := m.ensureActiveRewindWorkspace(); err != nil {
			warning = "aviso: el comando continuará sin snapshot de código para /rewind: " + err.Error() + "\n"
		}
		out, err := tools.Execute(context.Background(), "run_terminal_command",
			map[string]any{"command": command}, tools.Env{Root: root})
		if err != nil {
			out = "error: " + err.Error()
		}
		return bashResultMsg{output: warning + out}
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
		if isCreateFileTool(name) || name == "str_replace" {
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
	if isTodoToolName(c.Function.Name) {
		count := todoCallTaskCount(c.Function.Arguments)
		if count >= 0 {
			return fmt.Sprintf("$ todo_write %d tarea(s)", count)
		}
		return "$ todo_write"
	}
	if c.Function.Name == "Agent" || c.Function.Name == "Task" || c.Function.Name == "task" || c.Function.Name == "agent" {
		var args map[string]any
		if json.Unmarshal([]byte(c.Function.Arguments), &args) == nil {
			name, _ := args["subagent_type"].(string)
			desc, _ := args["description"].(string)
			if strings.TrimSpace(name) != "" {
				line := "$ @" + strings.TrimSpace(name)
				if strings.TrimSpace(desc) != "" {
					line += " " + strings.TrimSpace(desc)
				}
				return line
			}
		}
	}
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

func (m *ChatModel) invokeAgentDirect(name, prompt, visible string) (uikit.Model, uikit.Cmd) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		m.AddError("Uso: @" + name + " <tarea>")
		return m, nil
	}
	a := agents.Find(m.loadAgents(), name)
	if a == nil {
		m.AddError("Subagente no encontrado: " + name)
		return m, nil
	}
	return m.invokeAgentDefinition(*a, prompt, visible, "direct @"+a.Name)
}

func (m *ChatModel) invokeAgentDefinition(a agents.Agent, prompt, visible, description string) (uikit.Model, uikit.Cmd) {
	return m.invokeAgentDefinitionWithOptions(a, prompt, visible, description, false, false, "")
}

func (m *ChatModel) invokeAgentDefinitionWithBackground(a agents.Agent, prompt, visible, description string, background bool) (uikit.Model, uikit.Cmd) {
	return m.invokeAgentDefinitionWithOptions(a, prompt, visible, description, background, false, "")
}

func (m *ChatModel) invokeForkDefinitionWithBackground(a agents.Agent, prompt, visible, description string, background bool, isolation string) (uikit.Model, uikit.Cmd) {
	return m.invokeAgentDefinitionWithOptions(a, prompt, visible, description, background, true, isolation)
}

func (m *ChatModel) invokeAgentDefinitionWithOptions(a agents.Agent, prompt, visible, description string, background, fork bool, isolation string) (uikit.Model, uikit.Cmd) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return m, nil
	}
	// Capture the fork before the visible slash/@ invocation is appended. The
	// delegated prompt is added once by the child runtime and does not appear
	// twice in the inherited branch.
	selectedMode := m.selectedAgentMode()
	m.beginRewindPoint(visible)
	env := m.toolEnvWithAgentEvents("", selectedMode, m.agentEventSink())
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: visible, Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: visible})
	request := tools.AgentRequest{Agent: a, Prompt: prompt, Description: description, Model: a.Model, Background: background, Fork: fork, Isolation: isolation}
	if background && backgroundTasksAllowed() {
		m.persist()
		return m, func() uikit.Msg {
			_ = m.ensureActiveRewindWorkspace()
			result, err := env.RunAgent(m.sessionCtx, request)
			if err != nil {
				return systemMsg{text: "No se pudo iniciar @" + a.Name + " en background: " + err.Error()}
			}
			return systemMsg{text: "@" + a.Name + " ejecutándose en background · " + result.TaskID}
		}
	}
	// When background tasks are disabled through Claude's compatibility env,
	// execute synchronously with foreground policies and result semantics.
	request.Background = false
	if err := m.beginTurn(); err != nil {
		m.AddError(err.Error())
		return m, nil
	}
	turnID := m.activeTurnID
	runCtx := m.turnCtx
	m.streaming = true
	m.thinking = false
	m.working = true
	m.userScrolled = false
	m.persistTurnStart()
	m.refreshTranscript(true)
	execCmd := func() uikit.Msg {
		_ = m.ensureActiveRewindWorkspace()
		result, err := env.RunAgent(runCtx, request)
		return manualAgentResultMsg{turnID: turnID, agent: a.Name, taskID: result.TaskID, text: result.Text, err: err}
	}
	return m, execCmd
}

func backgroundTasksAllowed() bool {
	return strings.TrimSpace(os.Getenv("CLAUDE_CODE_DISABLE_BACKGROUND_TASKS")) != "1"
}

func (m *ChatModel) resetAgentSessionContext() {
	if m == nil {
		return
	}
	if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.sessionCtx, m.sessionCancel = context.WithCancel(context.Background())
	m.agentGeneration.Add(1)
}

func (m *ChatModel) agentEventSink() subagents.EventSink {
	if m == nil {
		return nil
	}
	generation := m.agentGeneration.Load()
	sessionCtx := m.sessionCtx
	return func(event subagents.Event) {
		if m.agentGeneration.Load() != generation || m.agentEventCh == nil {
			return
		}
		envelope := agentEventEnvelope{generation: generation, event: event}
		if sessionCtx == nil {
			m.agentEventCh <- envelope
			return
		}
		select {
		case m.agentEventCh <- envelope:
		case <-sessionCtx.Done():
		}
	}
}

func (m *ChatModel) applyAgentEventEnvelope(envelope agentEventEnvelope) bool {
	if m == nil || envelope.generation != m.agentGeneration.Load() {
		return false
	}
	m.applyAgentEvent(envelope.event)
	return true
}

// loadAgents discovers Claude-compatible subagents from bundled, user and
// project scopes. Only routing metadata enters the parent prompt; each agent's
// full Markdown body is loaded inside its isolated child context.
func (m *ChatModel) loadAgents() []agents.Agent {
	// Claude watches agent definitions during the session. Rescanning metadata is
	// cheap compared with a model request and means edits are picked up on the
	// next delegation without restarting Lilith.
	byName := map[string]agents.Agent{}
	for _, agent := range agents.Load(agents.DefaultLoadOptions(m.ctx.ConfigDir, m.project)) {
		byName[strings.ToLower(agent.Name)] = agent
	}
	for _, agent := range m.loadClaudePluginAgents() {
		byName[strings.ToLower(agent.Name)] = agent
	}
	m.agentCatalog = m.agentCatalog[:0]
	for _, agent := range byName {
		m.agentCatalog = append(m.agentCatalog, agent)
	}
	sort.Slice(m.agentCatalog, func(i, j int) bool {
		return strings.ToLower(m.agentCatalog[i].Name) < strings.ToLower(m.agentCatalog[j].Name)
	})
	return append([]agents.Agent(nil), m.agentCatalog...)
}

func (m *ChatModel) agentsBlock() string {
	return agents.FormatForPrompt(m.loadAgents())
}

func (m *ChatModel) loadSkillsForAgents() []skills.Skill {
	if !m.skillsEnabled() {
		return nil
	}
	return m.loadSkills()
}

// skillsEnabled devuelve el toggle persistido en settings.json (recargado
// cada vez: /config puede haberlo cambiado sin que el chat lo sepa).
func (m *ChatModel) skillsEnabled() bool {
	s, _ := config.Load(m.ctx.ConfigDir)
	return s.SkillsEnabled
}

// loadSkillsCatalog descubre el catálogo completo compatible de usuario +
// proyecto. Sólo inspecciona metadata SKILL.md; los recursos grandes se
// consultan después mediante skill_search/skill_files/skill_read.
func (m *ChatModel) loadSkillsCatalog() []skills.Skill {
	base := skills.Load(skills.DefaultLoadOptions(m.ctx.ConfigDir, m.project))
	settings, _ := config.Load(m.ctx.ConfigDir)
	if !settings.ClaudeCompatibilityEnabled {
		return base
	}
	// Legacy .claude/commands are explicit-only and lower precedence than real
	// Agent Skills with the same name. This keeps old Claude projects portable
	// without shadowing their modern SKILL.md replacements.
	byName := map[string]skills.Skill{}
	for _, sk := range skills.LoadLegacyCommands(m.ctx.ConfigDir, m.project) {
		byName[strings.ToLower(sk.Name)] = sk
	}
	for _, sk := range m.loadClaudePluginSkills() {
		byName[strings.ToLower(sk.Name)] = sk
	}
	for _, sk := range base {
		byName[strings.ToLower(sk.Name)] = sk
	}
	out := make([]skills.Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return skills.ApplyClaudeOverrides(m.ctx.ConfigDir, m.project, config.IsProjectTrusted(settings, m.project), out)
}

// loadSkills aplica las preferencias individuales de Lilith al catálogo. Una
// skill desactivada desaparece de activación automática, paleta, agentes e
// invocación manual, mientras el resto continúa disponible.
func (m *ChatModel) loadSkills() []skills.Skill {
	settings, _ := config.Load(m.ctx.ConfigDir)
	return filterEnabledSkills(settings, m.loadSkillsCatalog())
}

func filterEnabledSkills(settings config.Settings, catalog []skills.Skill) []skills.Skill {
	enabled := make([]skills.Skill, 0, len(catalog))
	for _, sk := range catalog {
		if config.IsSkillEnabled(settings, sk.Name) {
			enabled = append(enabled, sk)
		}
	}
	return enabled
}

// skillsBlock renderiza el bloque XML de skills disponibles cuando el toggle
// está activo. Devuelve "" si está desactivado o si no hay skills.
func (m *ChatModel) skillsBlock() string {
	if !m.skillsEnabled() {
		return ""
	}
	paths := instructionPathsFromHistory(m.history, m.project)
	all := m.loadSkills()
	visible := make([]skills.Skill, 0, len(all))
	for _, sk := range all {
		if skills.ApplicableToPaths(sk, paths) {
			visible = append(visible, sk)
		}
	}
	return skills.FormatForPrompt(visible)
}

// invokeSkill maneja "/skills:<nombre> [args]": lee SKILL.md, la inyecta como
// mensaje de usuario con instrucciones explícitas y arranca el turno. Si la
// skill no existe o los skills están desactivados, avisa por el chat.
func (m *ChatModel) invokeSkill(name, args string) uikit.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		m.AddError("Uso: /skills:<nombre> [instrucciones extra]")
		return nil
	}
	if !m.skillsEnabled() {
		m.AddError("Las skills están desactivadas. Actívalas en /config.")
		return nil
	}
	settings, _ := config.Load(m.ctx.ConfigDir)
	catalog := m.loadSkillsCatalog()
	sk := skills.Find(catalog, name)
	if sk == nil {
		m.AddError("Skill no encontrada: " + name + ". Revisa las carpetas de skills de Lilith/Claude/Agent del usuario o proyecto.")
		return nil
	}
	if !config.IsSkillEnabled(settings, sk.Name) {
		m.AddError("La skill " + name + " está desactivada. Actívala en /config > Skills.")
		return nil
	}
	if !sk.UserInvocable {
		m.AddError("La skill " + name + " no permite invocación manual (user-invocable: false).")
		return nil
	}
	body, err := skills.ReadContent(*sk)
	if err != nil {
		m.AddError("No se pudo leer la skill " + name + ": " + err.Error())
		return nil
	}
	body = skills.ExpandArguments(*sk, body, args)
	body, err = m.expandSkillShell(context.Background(), *sk, body)
	if err != nil {
		m.AddError("No se pudo preparar la skill " + name + ": " + err.Error())
		return nil
	}
	payload := skills.FormatInvocation(*sk, body, args)

	visible := "/" + sk.Name
	if strings.TrimSpace(args) != "" {
		visible += " " + args
	}
	if strings.EqualFold(sk.Context, "fork") {
		agentName := strings.TrimSpace(sk.Agent)
		if agentName == "" {
			agentName = "general-purpose"
		}
		a := agents.Find(m.loadAgents(), agentName)
		if a == nil {
			m.AddError("La skill requiere context: fork pero no existe el subagente " + agentName + ".")
			return nil
		}
		clone := *a
		clone.DisallowedTools = append(append([]string(nil), clone.DisallowedTools...), sk.DisallowedTools...)
		if strings.TrimSpace(sk.Model) != "" {
			clone.Model = sk.Model
		}
		if strings.TrimSpace(sk.Effort) != "" {
			clone.Effort = sk.Effort
		}
		if strings.TrimSpace(sk.HooksRaw) != "" && (sk.Source != "project" || m.skillAllowedToolsCanGrant(*sk)) {
			clone.HooksRaw = strings.TrimSpace(clone.HooksRaw + "\n" + sk.HooksRaw)
		}
		// Claude forked skills default to background unless explicitly false. The
		// Agent request carries this presentation/execution preference without
		// changing the portable agent definition itself.
		background := true
		if sk.BackgroundSet {
			background = sk.Background
		}
		_, cmd := m.invokeForkDefinitionWithBackground(clone, payload, visible, "skill "+sk.Name, background, "")
		return cmd
	}
	m.beginRewindPoint(visible)
	m.messages = append(m.messages, ChatMessage{Kind: MsgUser, Content: visible, Time: time.Now()})
	m.messages = append(m.messages, ChatMessage{Kind: MsgSystem, Content: "Skill cargada: " + sk.Name + " (" + sk.Source + ")", Time: time.Now()})
	m.appendHistory(openai.Message{Role: "user", Content: payload})
	selectedMode := m.selectedAgentMode()
	m.activeTools = m.selectToolsForPrompt(body+"\n"+args, selectedMode)
	// Una skill explícita puede ser pequeña, pero sus references/assets/scripts
	// pueden ser enormes. Mantén siempre disponible la navegación acotada nativa
	// para que el modelo no tenga que leer recursos completos ni usar shell.
	for _, name := range []string{"list_skills", "skill_search", "skill_files", "skill_read"} {
		if _, ok := tools.Get(name); ok {
			m.activeTools = appendUniqueTool(m.activeTools, name)
		}
	}
	if len(m.activeTools) == 0 {
		// Fallback defensivo; normalmente los cuatro tools de skill ya hacen que
		// este bloque no sea necesario.
		m.activeTools = []string{"tool_search", "list_skills", "skill_search", "skill_files", "skill_read"}
	}
	m.activeTools = m.rememberToolsForMode(selectedMode, m.activeTools)
	m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", selectedMode))
	sort.Strings(m.activeTools)
	m.toolFallback = ""
	if err := m.beginTurn(); err != nil {
		m.AddError(err.Error())
		return nil
	}
	if err := m.applySkillTurnOverrides(*sk); err != nil {
		m.endTurn()
		m.AddError(err.Error())
		return nil
	}
	m.materializeSkillAllowedTools(*sk)
	m.activeTools = tools.FilterAvailable(m.activeTools, m.toolEnv("", m.turnAgentMode))
	sort.Strings(m.activeTools)
	if m.turnAgentMode != planstate.Plan {
		m.cleanupCompletedTodos()
	}
	m.persistTurnStart()
	return uikit.Batch(m.runTurn(), m.chatMouseModeCmd())
}

// Kept in English on purpose: tool-use guidance is generally followed more
// consistently across providers in English. Full tool contracts stay in JSON
// schemas; the system prefix intentionally stays stable as lazy tools are
// materialized so provider prompt caching can reuse the long conversation.
func systemPrompt(activeTools []string, skillsBlock, agentsBlock, todoBlock, modeBlock string) string {
	base := "You are Lilith, an expert coding assistant operating inside the user's terminal. " +
		"Use the tools available for this turn to inspect and work on the real project. " +
		"Reply in the user's language (Spanish by default), while preserving code, paths, identifiers and shell syntax. " +
		"Be concise, direct, and keep working until the requested task is actually complete."
	if len(activeTools) == 0 {
		return base + skillsBlock + agentsBlock + todoBlock + modeBlock
	}

	// Keep this prefix independent from the exact lazy-tool set. Tool contracts
	// already travel in JSON schemas; repeating per-tool snippets here means that
	// every tool_search/materialization rewrites the system prefix and destroys
	// provider prompt-cache reuse. Pi documents the same cache footgun for
	// promptSnippet/promptGuidelines on lazily activated tools.
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nTool-use guidelines:\n")
	for _, rule := range []string{
		"Use only tool names present in the schemas for this turn; tool_search can discover additional capabilities when needed.",
		"Never write partial files or placeholders such as `...`, `// rest of code`, `TODO: fill in`, or equivalent; changes must leave real files usable as-is.",
		"For existing source files, prefer str_replace for precise replacements or apply_diff for unified patches. For str_replace, always send path plus both old and new, or a non-empty edits[] array; old must never be empty. Both tools validate against the current on-disk file; read when you need context or after a mismatch/ambiguity.",
		"Use write_file for complete generated documents or intentional full-file regeneration; existing targets require overwrite=true. Use append_file for long reports built in bounded sections. Never use shell heredocs for large file content.",
		"create_file is creation-only. The ambiguous legacy tool name `write` is unsupported.",
		"Treat FILE_EXISTS, OVERWRITE_REQUIRED, USE_CREATE_FILE and WRITE_BLOCKED as policy redirects and follow the result instead of repeating a rejected payload unchanged.",
		"When todo_write is available, use it for work with three or more meaningful implementation steps and keep its snapshot synchronized with actual progress.",
		"Before finishing code changes, run relevant build/tests when a safe terminal tool is available; never run destructive commands unless the user explicitly requested that destructive action.",
		"Preserve project conventions and unrelated content. Make the smallest safe change that satisfies the request.",
		"Do not stop with `do you want me to continue?` when you can keep working; ask only when genuinely blocked by missing information, credentials, or a destructive ambiguity.",
		"When finished, summarize concrete changes and any action still required from the user in 1-3 lines.",
	} {
		b.WriteString("- " + rule + "\n")
	}

	cwd, _ := os.Getwd()
	b.WriteString("\nCurrent working directory: " + filepath.ToSlash(cwd))
	b.WriteString(skillsBlock)
	b.WriteString(agentsBlock)
	b.WriteString(todoBlock)
	b.WriteString(modeBlock)
	return b.String()
}

func activeToolsSignature(names []string) string {
	if len(names) == 0 {
		return ""
	}
	copyNames := append([]string(nil), names...)
	sort.Strings(copyNames)
	return strings.Join(copyNames, "\x1f")
}

// streamPump reads one chunk and forwards it as a chatStreamMsg, keeping the
// channel handle so the next tick can continue pumping.
func streamPump(ch <-chan openai.Chunk, turnID, requestID uint64) uikit.Cmd {
	return func() uikit.Msg {
		c, ok := <-ch
		if !ok {
			return chatStreamMsg{turnID: turnID, requestID: requestID, done: true}
		}
		if c.Err != nil {
			return chatStreamMsg{turnID: turnID, requestID: requestID, err: c.Err}
		}
		if c.Retry != nil {
			return chatStreamMsg{turnID: turnID, requestID: requestID, ch: ch, retry: c.Retry}
		}
		if c.Done {
			return chatStreamMsg{turnID: turnID, requestID: requestID, done: true}
		}
		return chatStreamMsg{turnID: turnID, requestID: requestID, ch: ch, delta: c.Delta, toolCalls: c.ToolCalls, partial: c.Partial, superseded: c.SupersededIndices, thinking: c.Thinking, thinkingDone: c.ThinkingDone}
	}
}

func (m *ChatModel) View() string {
	w := m.ctx.Width
	if w <= 0 {
		w = 80
	}
	h := m.ctx.Height

	used, maxCtx := m.contextUsage()
	// El viewport persistido se usa para contenido/scroll, pero su altura puede
	// quedar obsoleta cuando cambia el cromo dinámico sin redimensionar la
	// terminal. Renderizamos una copia con la geometría exacta de ESTE frame.
	vp := m.viewportForFrame(w, h, used, maxCtx)
	transcript := vp.View()
	if bar := m.renderScrollbarFor(vp); bar != "" {
		transcript = tuistyle.JoinHorizontal(tuistyle.Top, transcript, bar)
	}

	chrome := m.bottomChromeView(w, used, maxCtx)
	if chrome == "" {
		return transcript
	}
	return transcript + "\n" + chrome
}

var _ uikit.Model = (*ChatModel)(nil)
