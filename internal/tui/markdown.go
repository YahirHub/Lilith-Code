package tui

import (
	"regexp"
	"strings"

	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

var inlineCodePattern = regexp.MustCompile("`([^`]+)`")

// RenderMarkdown renders the Markdown subset used in conversations without
// depending on an external Markdown renderer. The output is ANSI text consumed natively by tview.
func RenderMarkdown(src string, width int) string {
	src = strings.TrimRight(src, " \n\t")
	if src == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}

	theme := DefaultTheme()
	heading := tuistyle.NewStyle().Foreground(theme.Primary).Bold(true)
	strong := tuistyle.NewStyle().Foreground(theme.Foreground).Bold(true)
	code := tuistyle.NewStyle().Foreground(theme.Secondary).Background(theme.Surface)
	quote := tuistyle.NewStyle().Foreground(theme.Muted).Italic(true)
	muted := tuistyle.NewStyle().Foreground(theme.Muted)

	var out []string
	inFence := false
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if inFence {
				out = append(out, muted.Render("┌─ código"))
			} else {
				out = append(out, muted.Render("└─"))
			}
			continue
		}
		if inFence {
			out = append(out, code.Render("  "+raw))
			continue
		}

		prefix, body, style := "", raw, tuistyle.NewStyle()
		switch {
		case strings.HasPrefix(trimmed, "### "):
			prefix, body, style = "▸ ", strings.TrimSpace(trimmed[4:]), heading
		case strings.HasPrefix(trimmed, "## "):
			prefix, body, style = "◆ ", strings.TrimSpace(trimmed[3:]), heading
		case strings.HasPrefix(trimmed, "# "):
			prefix, body, style = "● ", strings.TrimSpace(trimmed[2:]), heading
		case strings.HasPrefix(trimmed, "> "):
			prefix, body, style = "│ ", strings.TrimSpace(trimmed[2:]), quote
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			prefix, body = "• ", strings.TrimSpace(trimmed[2:])
		default:
			body = raw
		}
		body = renderInlineMarkdown(body, strong, code)
		wrapped := wrapANSI(prefix+body, width)
		for i, line := range wrapped {
			if i > 0 && prefix != "" {
				line = strings.Repeat(" ", termansi.StringWidth(prefix)) + line
			}
			out = append(out, style.Render(line))
		}
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func renderInlineMarkdown(text string, strong, code tuistyle.Style) string {
	text = inlineCodePattern.ReplaceAllStringFunc(text, func(match string) string {
		return code.Render(strings.Trim(match, "`"))
	})
	// Conservative strong emphasis parser; unmatched markers remain visible.
	for {
		start := strings.Index(text, "**")
		if start < 0 {
			break
		}
		end := strings.Index(text[start+2:], "**")
		if end < 0 {
			break
		}
		end += start + 2
		text = text[:start] + strong.Render(text[start+2:end]) + text[end+2:]
	}
	return text
}

func wrapANSI(text string, width int) []string {
	if width < 1 {
		return []string{text}
	}
	return strings.Split(termansi.Wrap(text, width), "\n")
}
