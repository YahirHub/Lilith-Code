package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	compactctx "github.com/lilith/li/internal/compaction"
	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/tui/uikit"
)

type compactionResultMsg struct {
	id           uint64
	plan         compactctx.Plan
	auto         bool
	resumeTurn   bool
	instructions string
	summary      string
	err          error
}

func (m *ChatModel) compactionSelection() (*providers.Provider, string) {
	providerID := m.turnProvider
	modelID := m.turnModel
	if providerID == "" || modelID == "" {
		active := m.ctx.Providers.Active()
		providerID = active.ProviderID
		modelID = active.ModelID
	}
	if providerID == "" || modelID == "" {
		return nil, ""
	}
	return m.ctx.Providers.FindProvider(providerID), modelID
}

func (m *ChatModel) compactionPlan(forceOlder bool) (compactctx.Plan, bool) {
	contextWindow := 0
	if provider, modelID := m.compactionSelection(); provider != nil && modelID != "" {
		contextWindow = provider.ContextWindow(modelID)
	}

	// Select the cut using the same old-tool pruning applied to real requests.
	// The indices remain identical, then Head/Tail are replaced with exact
	// durable messages so the summary/archive never lose original details.
	estimatedHistory := compactHistoryForRequest(m.history)
	plan, ok := compactctx.Prepare(estimatedHistory, compactctx.RecentTokenBudget(contextWindow), compactctx.MinimumRecentUserTurns)
	if !ok && forceOlder {
		// The request can exceed the threshold because of system instructions or
		// tool schemas even when all history fits inside the normal recent-tail
		// budget. In that case preserve the newest complete user turn and compact
		// every older turn rather than entering a no-op preflight loop.
		plan, ok = compactctx.PrepareOlderTurns(estimatedHistory)
	}
	if !ok {
		return compactctx.Plan{}, false
	}
	start := 0
	if len(m.history) > 0 {
		if _, previous := compactctx.SummaryFromMessage(m.history[0]); previous {
			start = 1
		}
	}
	if plan.CutIndex < start || plan.CutIndex > len(m.history) {
		return compactctx.Plan{}, false
	}
	plan.Head = cloneHistoryMessages(m.history[start:plan.CutIndex])
	plan.Tail = cloneHistoryMessages(m.history[plan.CutIndex:])
	plan.TokensBefore = compactctx.EstimateTokens(estimatedHistory)
	plan.TailTokens = compactctx.EstimateTokens(estimatedHistory[plan.CutIndex:])
	return plan, true
}

func (m *ChatModel) shouldAutoCompact() (compactctx.Plan, bool) {
	if m.compacting || len(m.history) == 0 || m.autoCompactionSkipHistoryLen == len(m.history) {
		return compactctx.Plan{}, false
	}
	provider, modelID := m.compactionSelection()
	if provider == nil || modelID == "" {
		return compactctx.Plan{}, false
	}
	mode := m.turnAgentMode
	if mode == "" {
		mode = m.effectiveAgentMode()
	}
	prepared := m.prepareRequestMessages(mode)
	schemas := m.requestToolSchemas(mode)
	if !compactctx.NeedsAutoRequest(prepared, schemas, provider.ContextWindow(modelID), compactctx.DefaultReserveTokens) {
		return compactctx.Plan{}, false
	}
	plan, ok := m.compactionPlan(true)
	if !ok {
		// Avoid an infinite preflight loop when the protected recent tail itself
		// occupies the window. A new history entry will allow another attempt.
		m.autoCompactionSkipHistoryLen = len(m.history)
		return compactctx.Plan{}, false
	}
	return plan, true
}

func (m *ChatModel) runCompactCommand(instructions string) uikit.Cmd {
	if m.compacting {
		m.AddSystem("Ya hay una compactación en curso.")
		return nil
	}
	if m.activeTurnID != 0 || m.streaming {
		m.AddError("/compact sólo puede iniciarse cuando el agente está en reposo. Usa Esc para detener el turno actual y vuelve a intentarlo.")
		return nil
	}
	provider, modelID := m.compactionSelection()
	if provider == nil || modelID == "" {
		m.AddError("No hay un proveedor/modelo activo para resumir la conversación.")
		return nil
	}
	plan, ok := m.compactionPlan(true)
	if !ok {
		m.AddSystem("Todavía no hay un turno anterior que pueda resumirse sin perder la solicitud actual.")
		return nil
	}
	return m.startCompaction(plan, *provider, modelID, false, false, instructions, nil)
}

func (m *ChatModel) startAutoCompaction(plan compactctx.Plan) uikit.Cmd {
	provider, modelID := m.compactionSelection()
	if provider == nil || modelID == "" {
		return nil
	}
	return m.startCompaction(plan, *provider, modelID, true, true, "", m.turnCtx)
}

func (m *ChatModel) startCompaction(plan compactctx.Plan, provider providers.Provider, modelID string, auto, resumeTurn bool, instructions string, parent context.Context) uikit.Cmd {
	if m.compacting || len(plan.Head) == 0 {
		return nil
	}
	if parent == nil {
		parent = m.sessionCtx
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m.compactionSeq++
	id := m.compactionSeq
	m.activeCompactionID = id
	m.compactionCancel = cancel
	m.compacting = true
	m.streaming = true
	m.thinking = true
	m.working = false
	m.thinkingFrame = 0
	m.refreshTranscript(true)

	client := m.ctx.Client
	if client == nil {
		client = openai.NewClient(m.ctx.ConfigDir)
	}
	req := openai.Request{
		Provider: provider,
		Model:    modelID,
		Messages: compactctx.BuildSummaryMessages(plan, instructions, provider.ContextWindow(modelID)),
		Stream:   true,
	}
	return uikit.Batch(func() uikit.Msg {
		summary, err := collectCompactionSummary(ctx, client, req)
		return compactionResultMsg{
			id: id, plan: plan, auto: auto, resumeTurn: resumeTurn,
			instructions: instructions, summary: summary, err: err,
		}
	}, thinkingTick(0))
}

func collectCompactionSummary(ctx context.Context, client *openai.Client, req openai.Request) (string, error) {
	if client == nil {
		return "", errors.New("cliente de proveedor no disponible")
	}
	var b strings.Builder
	for chunk := range client.Stream(ctx, req) {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
		}
	}
	summary := strings.TrimSpace(b.String())
	if summary == "" {
		return "", errors.New("el proveedor no devolvió un resumen")
	}
	return summary, nil
}

func (m *ChatModel) cancelManualCompaction() {
	if !m.compacting || m.activeTurnID != 0 {
		return
	}
	m.activeCompactionID = 0
	if m.compactionCancel != nil {
		m.compactionCancel()
	}
	m.compactionCancel = nil
	m.compacting = false
	m.streaming = false
	m.thinking = false
	m.working = false
	m.AddSystem("Compactación cancelada.")
}

func (m *ChatModel) stopCompactionState() {
	if m.compactionCancel != nil {
		m.compactionCancel()
	}
	m.compactionCancel = nil
	m.compacting = false
	m.activeCompactionID = 0
}

func (m *ChatModel) applyCompactionResult(v compactionResultMsg) uikit.Cmd {
	if v.id == 0 || v.id != m.activeCompactionID {
		return nil
	}
	resume := v.resumeTurn && m.activeTurnID != 0 && m.turnCtx != nil && m.turnCtx.Err() == nil
	m.stopCompactionState()
	m.thinking = false
	m.working = false

	if v.err != nil {
		if errors.Is(v.err, context.Canceled) {
			if !resume {
				m.streaming = false
			}
			return nil
		}
		m.autoCompactionSkipHistoryLen = len(m.history)
		if v.auto {
			m.AddError("No se pudo compactar automáticamente el contexto: " + v.err.Error() + ". Se intentará continuar con el historial actual.")
			if resume {
				m.streaming = true
				return m.runTurn()
			}
		} else {
			m.streaming = false
			m.AddError("No se pudo compactar la conversación: " + v.err.Error())
		}
		return nil
	}

	archivedEnd := v.plan.CutIndex
	if archivedEnd < 0 {
		archivedEnd = 0
	}
	if archivedEnd > len(m.history) {
		archivedEnd = len(m.history)
	}
	archived := cloneHistoryMessages(m.history[:archivedEnd])
	// Commands/events are serialized on the TUI loop, but detached workers may
	// append protocol messages while the summarizer is running. Preserve every
	// message added after the snapshot used to build the plan.
	extraStart := v.plan.SourceLength
	if extraStart < 0 {
		extraStart = 0
	}
	if extraStart > len(m.history) {
		extraStart = len(m.history)
	}
	extra := cloneHistoryMessages(m.history[extraStart:])
	m.history = compactctx.Apply(v.plan, v.summary)
	m.history = append(m.history, extra...)
	m.invalidateContextUsage()
	m.invalidateTranscriptCache()
	// Re-evaluate the rebuilt request. One pass normally suffices, but very
	// large system/tool-schema overhead may require a second safe reduction from
	// two retained turns to one. The no-plan guard stops once nothing older can
	// be compacted.
	m.autoCompactionSkipHistoryLen = 0

	tokensAfter := compactctx.EstimateTokens(m.history)
	if m.sess != nil {
		m.sess.Compactions = append(m.sess.Compactions, session.CompactionRecord{
			ID:               fmt.Sprintf("compact-%d", time.Now().UnixNano()),
			CreatedAt:        time.Now(),
			Auto:             v.auto,
			Instructions:     strings.TrimSpace(v.instructions),
			Summary:          strings.TrimSpace(v.summary),
			TokensBefore:     v.plan.TokensBefore,
			TokensAfter:      tokensAfter,
			SplitTurn:        v.plan.SplitTurn,
			FullCompaction:   v.plan.FullCompaction,
			ArchivedMessages: archived,
		})
	}
	kind := "manual"
	if v.auto {
		kind = "automática"
	}
	detail := ""
	if v.plan.FullCompaction {
		detail = " Se resumió el contexto activo completo porque no existía un corte reciente seguro."
	} else if v.plan.SplitTurn {
		detail = " Se resumió también el prefijo de un turno largo, conservando una frontera segura de assistant/tool."
	}
	m.messages = append(m.messages, ChatMessage{
		Kind:    MsgSystem,
		Content: fmt.Sprintf("Compactación %s completada: contexto activo estimado %d → %d tokens.%s El transcript visible y el historial archivado permanecen completos.", kind, v.plan.TokensBefore, tokensAfter, detail),
		Time:    time.Now(),
	})
	m.persist()
	m.refreshTranscript(true)

	if resume {
		m.streaming = true
		m.thinking = true
		return m.runTurn()
	}
	m.streaming = false
	return m.drainFollowUp()
}

func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"context length", "context window", "maximum context", "max context",
		"too many tokens", "token limit", "prompt is too long", "input is too long",
		"context_length_exceeded", "context overflow", "exceeds the model's context",
	} {
		if strings.Contains(low, fragment) {
			return true
		}
	}
	return false
}

func (m *ChatModel) recoverFromContextOverflow(err error) uikit.Cmd {
	if !isContextOverflowError(err) || m.compacting {
		return nil
	}
	if m.autoCompactionSkipHistoryLen == len(m.history) {
		return nil
	}
	m.activeRequestID = 0
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	// A provider can theoretically emit a partial assistant response before a
	// terminal overflow error. Promote it first, then calculate the cut so that
	// neither the summary nor the exact retained tail silently drops that text.
	m.checkpointPartialAssistantHistory()
	m.finishThinkingPanel()
	m.pendingCall = nil
	m.assistantActive = -1
	plan, ok := m.compactionPlan(true)
	if !ok {
		return nil
	}
	m.autoCompactionSkipHistoryLen = 0
	return m.startAutoCompaction(plan)
}
