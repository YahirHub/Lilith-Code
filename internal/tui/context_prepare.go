package tui

import (
	"encoding/json"
	"strings"

	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/subagents"
	"github.com/lilith/li/internal/tools"
)

const (
	// Keep the current and previous user turns verbatim. Older tool payloads are
	// re-fetchable and are the fastest-growing part of coding-agent context.
	requestProtectedUserTurns = 2
	requestToolOutputMaxChars = 2000
	requestToolArgsMaxChars   = 2000
)

// prepareRequestMessages builds the model-facing context without mutating the
// durable session. The transcript/history on disk remains complete; only the
// wire copy sheds stale, re-fetchable tool payloads.
func (m *ChatModel) prepareRequestMessages(mode planstate.Mode) []openai.Message {
	history := compactHistoryForRequest(m.history)
	bundle := m.instructionBundle()
	conditional := bundle.ConditionalPrompt(instructionPathsFromHistory(history, m.project))
	runtime := strings.TrimSpace(m.todoBlockForMode(mode) + m.promptModeBlock(mode) + m.goalPromptBlock() + conditional)
	if runtime != "" {
		history = appendRuntimeState(history, runtime)
	}

	msgs := make([]openai.Message, 0, len(history)+2)
	// Keep the reusable prefix stable. Todo/Plan/path-scoped state belongs near
	// the current turn; persistent project instructions sit immediately after the
	// system prompt just like Claude Code's CLAUDE.md user-message layer.
	msgs = append(msgs, openai.Message{
		Role:    "system",
		Content: systemPrompt(m.activeTools, m.skillsBlock(), m.agentsBlock(), "", ""),
	})
	if projectInstructions := strings.TrimSpace(bundle.StaticPrompt()); projectInstructions != "" {
		msgs = append(msgs, openai.Message{Role: "user", Content: projectInstructions})
	}
	if memoryPrompt := strings.TrimSpace(m.mainMemoryPrompt()); memoryPrompt != "" {
		msgs = append(msgs, openai.Message{Role: "user", Content: memoryPrompt})
	}
	msgs = append(msgs, history...)
	return msgs
}

// requestToolSchemas builds exactly the tool-schema payload sent with a model
// request. Keeping this in one helper prevents auto-compaction/context usage
// from estimating a different request than runTurn eventually submits.
func (m *ChatModel) requestToolSchemas(mode planstate.Mode) []any {
	var schemas []any
	for _, schema := range tools.Schemas(m.activeTools) {
		schemas = append(schemas, schema)
	}
	return append(schemas, m.mcpSchemas(mode)...)
}

// forkContextMessages returns a protocol-valid snapshot for Claude-compatible
// conversation forks. During an Agent tool call the durable history already
// contains the assistant tool_call but not its result; carrying that dangling
// call into a child would make the provider reject the request.
func (m *ChatModel) forkContextMessages(mode planstate.Mode) []openai.Message {
	return subagents.SanitizeForkMessages(m.prepareRequestMessages(mode))
}

func compactHistoryForRequest(history []openai.Message) []openai.Message {
	if len(history) == 0 {
		return nil
	}
	out := cloneHistoryMessages(history)
	protectFrom := protectedTailStart(out, requestProtectedUserTurns)
	for i := 0; i < protectFrom; i++ {
		switch out[i].Role {
		case "tool":
			if keepToolResultVerbatim(out[i].Name) {
				continue
			}
			out[i].Content = compactText(out[i].Content, requestToolOutputMaxChars, "older tool output")
		case "assistant":
			for j := range out[i].ToolCalls {
				out[i].ToolCalls[j].Function.Arguments = compactToolArguments(out[i].ToolCalls[j].Function.Arguments)
			}
		}
	}
	return out
}

func protectedTailStart(history []openai.Message, userTurns int) int {
	if userTurns <= 0 {
		return len(history)
	}
	seen := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		seen++
		if seen == userTurns {
			return i
		}
	}
	return 0
}

func keepToolResultVerbatim(name string) bool {
	switch name {
	case "skill_read", "todo_write", "plan_question", "plan_exit":
		return true
	default:
		return false
	}
}

func compactText(text string, maxChars int, label string) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	head := maxChars * 3 / 5
	tail := maxChars - head
	omitted := len(text) - head - tail
	return text[:head] + "\n\n[" + label + " compacted: " + itoa(omitted) + " chars omitted; re-run the tool if exact content is needed]\n\n" + text[len(text)-tail:]
}

func compactToolArguments(raw string) string {
	if len(raw) <= requestToolArgsMaxChars {
		return raw
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return compactText(raw, requestToolArgsMaxChars, "older tool input")
	}
	for _, key := range []string{"content", "old", "new", "patch", "diff", "replacement"} {
		value, ok := args[key].(string)
		if !ok || len(value) <= 256 {
			continue
		}
		args[key] = "[older tool input compacted; inspect the current file again if exact content is needed]"
	}
	encoded, err := json.Marshal(args)
	if err == nil && len(encoded) <= requestToolArgsMaxChars {
		return string(encoded)
	}

	minimal := map[string]any{"note": "older tool input compacted"}
	for _, key := range []string{"path", "file", "query", "pattern", "command"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			minimal[key] = compactText(value, 512, "value")
		}
	}
	encoded, err = json.Marshal(minimal)
	if err != nil {
		return `{"note":"older tool input compacted"}`
	}
	return string(encoded)
}

func appendRuntimeState(history []openai.Message, runtime string) []openai.Message {
	if strings.TrimSpace(runtime) == "" {
		return history
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		history[i].Content += "\n\n<lilith_runtime_state>\n" + runtime + "\n</lilith_runtime_state>"
		return history
	}
	return history
}

// Tiny allocation-free-ish helper; avoids pulling fmt into a hot context path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
