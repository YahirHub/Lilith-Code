package tui

import (
	"github.com/lilith/li/internal/interaction"
	"github.com/lilith/li/internal/tui/uikit"
)

// interactionRequestMsg and interactionResolvedMsg are process-local UI
// messages. Secret values never enter the transcript, session or model input.
type interactionRequestMsg struct{ request *interaction.Request }
type interactionResolvedMsg struct {
	request *interaction.Request
	result  interaction.Result
}

func waitInteractionCmd(bridge interface {
	Next() (*interaction.Request, bool)
}) uikit.Cmd {
	if bridge == nil {
		return nil
	}
	return func() uikit.Msg {
		req, ok := bridge.Next()
		if !ok {
			return nil
		}
		return interactionRequestMsg{request: req}
	}
}
