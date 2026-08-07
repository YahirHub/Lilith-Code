package tui

import (
	"context"
	"testing"
)

// primeTestRequest creates the same turn/request identity that production
// streamPump attaches to every asynchronous provider event. Tests that inject
// chatStreamMsg directly must do this too; zero IDs are intentionally rejected
// because accepting anonymous async events would reopen the stale-event race
// that can resume a canceled agent.
func primeTestRequest(t *testing.T, m *ChatModel) {
	t.Helper()
	if m.activeTurnID == 0 || m.turnCtx == nil || m.turnCtx.Err() != nil {
		m.turnSeq++
		m.activeTurnID = m.turnSeq
		m.turnCtx, m.cancel = context.WithCancel(context.Background())
	}
	// Production snapshots provider/model in beginTurnMode before any request.
	// Tests that later re-enter runTurn (for example provider recovery) need the
	// same snapshot; otherwise runTurn correctly rejects the synthetic turn as
	// provider-less and returns nil, which makes the recovery test exercise an
	// impossible state instead of the production path.
	if m.ctx != nil && (m.turnProvider == "" || m.turnModel == "") {
		active := m.ctx.Providers.Active()
		if m.turnProvider == "" {
			m.turnProvider = active.ProviderID
		}
		if m.turnModel == "" {
			m.turnModel = active.ModelID
		}
	}
	m.requestSeq++
	m.activeRequestID = m.requestSeq
	m.streaming = true
	t.Cleanup(func() {
		if m.cancel != nil {
			m.cancel()
		}
	})
}

func activeStreamMsg(m *ChatModel, msg chatStreamMsg) chatStreamMsg {
	msg.turnID = m.activeTurnID
	msg.requestID = m.activeRequestID
	return msg
}

func TestPrimeTestRequestSnapshotsActiveProviderForReentrantRunTurn(t *testing.T) {
	m := newInputTestChat(t)
	primeTestRequest(t, m)

	if m.turnProvider != "test" || m.turnModel != "modelo" {
		t.Fatalf("primeTestRequest debe reflejar beginTurnMode: provider=%q model=%q", m.turnProvider, m.turnModel)
	}
}
