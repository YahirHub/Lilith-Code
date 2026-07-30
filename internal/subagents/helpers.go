package subagents

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/lilith/li/internal/providers/openai"
)

func filepathClean(s string) string { return filepath.Clean(s) }
func filepathSlash(s string) string { return filepath.ToSlash(filepath.Clean(s)) }
func timeNow() time.Time            { return time.Now() }

func cloneMessages(messages []openai.Message) []openai.Message {
	out := make([]openai.Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].ToolCalls = append([]openai.ToolCall(nil), messages[i].ToolCalls...)
	}
	return out
}

// sanitizeForkMessages removes a trailing unresolved assistant tool call. A
// fork may be created from inside that call, before the parent has appended its
// tool result, and OpenAI-compatible APIs reject such dangling protocol state.
func SanitizeForkMessages(messages []openai.Message) []openai.Message {
	out := cloneMessages(messages)
	for i, msg := range out {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		pending := make(map[string]struct{}, len(msg.ToolCalls))
		unresolved := false
		for _, call := range msg.ToolCalls {
			if strings.TrimSpace(call.ID) == "" {
				unresolved = true
				continue
			}
			pending[call.ID] = struct{}{}
		}
		for j := i + 1; j < len(out) && len(pending) > 0; j++ {
			if out[j].Role != "tool" {
				break
			}
			delete(pending, out[j].ToolCallID)
		}
		if !unresolved && len(pending) == 0 {
			continue
		}
		trimmed := append([]openai.Message(nil), out[:i]...)
		if strings.TrimSpace(msg.Content) != "" || strings.TrimSpace(msg.ReasoningContent) != "" {
			msg.ToolCalls = nil
			trimmed = append(trimmed, msg)
		}
		return trimmed
	}
	return out
}
