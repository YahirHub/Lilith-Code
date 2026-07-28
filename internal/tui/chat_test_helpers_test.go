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
