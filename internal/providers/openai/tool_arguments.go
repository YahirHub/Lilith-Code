package openai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SanitizeToolCallArguments returns a valid JSON object string suitable for
// function.arguments. Some OpenAI-compatible gateways decode escaped control
// characters from their outer JSON envelope into literal bytes inside the
// nested arguments JSON. That leaves the outer response valid while poisoning
// the next request with an invalid JSON object string.
//
// Valid objects are preserved byte-for-byte apart from surrounding whitespace.
// Literal ASCII control characters inside JSON strings are re-escaped without
// changing their semantic value. Irrecoverably malformed historical arguments
// degrade to an empty object so one bad tool call cannot permanently brick a
// resumable conversation.
func SanitizeToolCallArguments(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "{}"
	}
	if isJSONObject(trimmed) {
		return trimmed
	}

	repaired := escapeJSONStringControls(trimmed)
	if isJSONObject(repaired) {
		return repaired
	}
	return "{}"
}

// SanitizeToolCalls clones calls and makes every function.arguments value safe
// to persist and send back to a provider.
func SanitizeToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := append([]ToolCall(nil), calls...)
	for i := range out {
		out[i].Function.Arguments = SanitizeToolCallArguments(out[i].Function.Arguments)
	}
	return out
}

// SanitizeMessages clones a protocol history and repairs nested tool argument
// JSON. It is intentionally side-effect free so callers can sanitize a request
// without mutating the persisted session currently being rendered.
func SanitizeMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := append([]Message(nil), messages...)
	for i := range out {
		if len(out[i].ToolCalls) > 0 {
			out[i].ToolCalls = SanitizeToolCalls(out[i].ToolCalls)
		}
	}
	return out
}

func isJSONObject(raw string) bool {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return false
	}
	_, ok := value.(map[string]any)
	return ok
}

func escapeJSONStringControls(raw string) string {
	var b strings.Builder
	b.Grow(len(raw) + 16)
	inString := false
	escaped := false

	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}

		if escaped {
			if c < 0x20 {
				writeJSONControlEscape(&b, c, false)
			} else {
				b.WriteByte(c)
			}
			escaped = false
			continue
		}

		switch {
		case c == '\\':
			b.WriteByte(c)
			escaped = true
		case c == '"':
			b.WriteByte(c)
			inString = false
		case c < 0x20:
			writeJSONControlEscape(&b, c, true)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func writeJSONControlEscape(b *strings.Builder, c byte, includeSlash bool) {
	if includeSlash {
		b.WriteByte('\\')
	}
	switch c {
	case '\b':
		b.WriteByte('b')
	case '\f':
		b.WriteByte('f')
	case '\n':
		b.WriteByte('n')
	case '\r':
		b.WriteByte('r')
	case '\t':
		b.WriteByte('t')
	default:
		if !includeSlash {
			// The original backslash was already copied by the caller.
			b.WriteByte('u')
		} else {
			b.WriteByte('u')
		}
		fmt.Fprintf(b, "%04x", c)
	}
}
