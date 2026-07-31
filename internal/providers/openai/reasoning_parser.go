package openai

import "strings"

// reasoningPiece is one visible fragment after separating provider-specific
// inline reasoning markup from the assistant's final answer.
type reasoningPiece struct {
	Text     string
	Thinking bool
}

type reasoningMarker struct {
	open  string
	close string
}

var inlineReasoningMarkers = []reasoningMarker{
	{open: "<think>", close: "</think>"},
	{open: "<thinking>", close: "</thinking>"},
	{open: "<analysis>", close: "</analysis>"},
	{open: "<reasoning>", close: "</reasoning>"},
	{open: "<thought>", close: "</thought>"},
	{open: "[think]", close: "[/think]"},
	{open: "[analysis]", close: "[/analysis]"},
	{open: "[reasoning]", close: "[/reasoning]"},
	// Harmony-style responses used by some OpenAI-compatible runtimes.
	{open: "<|channel|>analysis<|message|>", close: "<|channel|>final<|message|>"},
	{open: "<|start|>assistant<|channel|>analysis<|message|>", close: "<|end|>"},
}

// reasoningStreamParser is deliberately independent from SSE framing. It can
// consume arbitrary fragments, including opening/closing markers split across
// network chunks, and emits only text that is safe to display.
type reasoningStreamParser struct {
	buffer   string
	thinking bool
	marker   reasoningMarker
}

func (p *reasoningStreamParser) Feed(fragment string) []reasoningPiece {
	if fragment == "" {
		return nil
	}
	p.buffer += fragment
	return p.drain(false)
}

func (p *reasoningStreamParser) Flush() []reasoningPiece {
	return p.drain(true)
}

func (p *reasoningStreamParser) drain(flush bool) []reasoningPiece {
	var out []reasoningPiece
	for p.buffer != "" {
		if p.thinking {
			idx := indexFold(p.buffer, p.marker.close)
			if idx >= 0 {
				appendReasoningPiece(&out, p.buffer[:idx], true)
				p.buffer = p.buffer[idx+len(p.marker.close):]
				p.thinking = false
				p.marker = reasoningMarker{}
				continue
			}
			if flush {
				// A provider that opened a reasoning block but never closed it is
				// still reasoning. Do not leak the markup into the final answer.
				appendReasoningPiece(&out, p.buffer, true)
				p.buffer = ""
				break
			}
			keep := longestMarkerPrefixSuffix(p.buffer, []string{p.marker.close})
			cut := len(p.buffer) - keep
			if cut == 0 {
				break
			}
			appendReasoningPiece(&out, p.buffer[:cut], true)
			p.buffer = p.buffer[cut:]
			break
		}

		idx, marker := earliestOpeningMarker(p.buffer)
		if idx >= 0 {
			appendReasoningPiece(&out, p.buffer[:idx], false)
			p.buffer = p.buffer[idx+len(marker.open):]
			p.thinking = true
			p.marker = marker
			continue
		}
		if flush {
			appendReasoningPiece(&out, p.buffer, false)
			p.buffer = ""
			break
		}
		opens := make([]string, 0, len(inlineReasoningMarkers))
		for _, marker := range inlineReasoningMarkers {
			opens = append(opens, marker.open)
		}
		keep := longestMarkerPrefixSuffix(p.buffer, opens)
		cut := len(p.buffer) - keep
		if cut == 0 {
			break
		}
		appendReasoningPiece(&out, p.buffer[:cut], false)
		p.buffer = p.buffer[cut:]
		break
	}
	return out
}

func appendReasoningPiece(out *[]reasoningPiece, text string, thinking bool) {
	if text == "" {
		return
	}
	if n := len(*out); n > 0 && (*out)[n-1].Thinking == thinking {
		(*out)[n-1].Text += text
		return
	}
	*out = append(*out, reasoningPiece{Text: text, Thinking: thinking})
}

func earliestOpeningMarker(s string) (int, reasoningMarker) {
	best := -1
	var selected reasoningMarker
	for _, marker := range inlineReasoningMarkers {
		idx := indexFold(s, marker.open)
		if idx < 0 || (best >= 0 && idx >= best) {
			continue
		}
		best = idx
		selected = marker
	}
	return best, selected
}

func indexFold(s, needle string) int {
	if needle == "" {
		return 0
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		// All supported markers start with an ASCII delimiter. Skipping other
		// bytes avoids slicing through a multibyte rune and keeps the returned
		// byte offset valid for the original string.
		if s[i] != needle[0] {
			continue
		}
		if strings.EqualFold(s[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func longestMarkerPrefixSuffix(s string, markers []string) int {
	best := 0
	for _, marker := range markers {
		max := len(marker) - 1
		if len(s) < max {
			max = len(s)
		}
		for n := max; n > best; n-- {
			if strings.EqualFold(s[len(s)-n:], marker[:n]) {
				best = n
				break
			}
		}
	}
	return best
}
