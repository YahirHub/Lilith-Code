package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// CommandPanel is the live terminal-style window used for
// run_terminal_command tool calls. It mirrors the bash-like UX from pi.dev:
//   * BLUE  border/title while the model is still streaming the command or
//     while the shell is running (pending / executing).
//   * GREEN border once the process exits with code 0.
//   * RED   border on non-zero exit codes, timeouts, or transport errors.
// Output (stdout + stderr) is shown compactly with a preview window and a
// "N earlier lines" hint so long runs don't blow up the transcript.
type CommandPanel struct {
	CallID    string
	Index     int
	Command   string // streamed from the tool call arguments
	Timeout   int    // seconds, if the model set it
	Done      bool
	Failed    bool
	Superseded bool
	ExitCode  int
	Stdout    string
	Stderr    string
	TimedOut  bool
	StartedAt time.Time
	Elapsed   time.Duration
	Expanded  bool
}

// IsCommandTool reports the tools rendered with a CommandPanel.
func IsCommandTool(name string) bool { return name == "run_terminal_command" }

// Start marks the moment the tool call was dispatched to the runner.
func (p *CommandPanel) Start() {
	if p.StartedAt.IsZero() {
		p.StartedAt = time.Now()
	}
}

// Update refreshes the streamed command from a partial JSON args blob.
func (p *CommandPanel) Update(rawArgs string) {
	if v, ok := partialJSONString(rawArgs, "command"); ok {
		p.Command = v
	}
	if v, ok := partialJSONNumber(rawArgs, "timeout_seconds"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			p.Timeout = n
		}
	}
}

var exitCodeRe = regexp.MustCompile(`(?m)^exit_code:\s*(-?\d+)`)

// Finish closes the panel with the shell's real output.
func (p *CommandPanel) Finish(result string) {
	p.Done = true
	if !p.StartedAt.IsZero() {
		p.Elapsed = time.Since(p.StartedAt)
	}
	trim := strings.TrimSpace(result)
	if strings.HasPrefix(trim, "error:") {
		p.Failed = true
		p.Stderr = sanitizeOutput(trim)
		p.ExitCode = -1
		return
	}
	if m := exitCodeRe.FindStringSubmatch(result); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			p.ExitCode = n
		}
	}
	if strings.Contains(result, "\ntimeout: yes") || strings.HasPrefix(result, "timeout: yes") {
		p.TimedOut = true
	}
	p.Stdout = sanitizeOutput(extractBlock(result, "stdout:"))
	p.Stderr = sanitizeOutput(extractBlock(result, "stderr:"))
	p.Failed = p.ExitCode != 0 || p.TimedOut
}

// MarkSuperseded is called when the backend abandons this tool call in the
// middle of streaming (Codex server-side retry).
func (p *CommandPanel) MarkSuperseded() {
	p.Done = true
	p.Superseded = true
	p.Failed = false
	p.Expanded = false
}

// extractBlock returns the trailing block after `header` up to the next
// blank line or end-of-string. The formatter in tools/exec.go emits
// `stdout:\n<...>\n` and `stderr:\n<...>\n` blocks separated by newlines.
func extractBlock(text, header string) string {
	idx := strings.Index(text, header)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(header):]
	rest = strings.TrimPrefix(rest, "\n")
	// Block ends at the next known header (stdout: / stderr:) if present,
	// otherwise at end of string.
	end := len(rest)
	for _, h := range []string{"\nstdout:", "\nstderr:"} {
		if i := strings.Index(rest, h); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimRight(rest[:end], "\n")
}

// cmdPreviewLines is the fixed height of the output preview inside the panel.
const cmdPreviewLines = 10

// View renders the panel. `selected` shows the ctrl+o hint.
func (p *CommandPanel) View(s Styles, width int, selected bool) string {
	if width < 24 {
		width = 24
	}
	t := s.Theme
	inner := width - 4

	// Border + accent color depending on state.
	var borderColor lipgloss.Color
	var tag string
	var tagStyle lipgloss.Style
	switch {
	case p.Superseded:
		borderColor = t.Muted
		tag = "retried"
		tagStyle = lipgloss.NewStyle().Foreground(t.Background).Background(t.Muted).Padding(0, 1).Bold(true)
	case !p.Done:
		borderColor = t.Info // blue while streaming/running
		tag = "running"
		tagStyle = lipgloss.NewStyle().Foreground(t.Background).Background(t.Info).Padding(0, 1).Bold(true)
	case p.Failed:
		borderColor = t.Danger
		if p.TimedOut {
			tag = "timeout"
		} else {
			tag = fmt.Sprintf("exit %d", p.ExitCode)
		}
		tagStyle = lipgloss.NewStyle().Foreground(t.Background).Background(t.Danger).Padding(0, 1).Bold(true)
	default:
		borderColor = t.Success
		tag = "exit 0"
		tagStyle = lipgloss.NewStyle().Foreground(t.Background).Background(t.Success).Padding(0, 1).Bold(true)
	}
	if selected && !p.Done {
		borderColor = t.Info
	}

	// Title: `$ <command>` (streamed live). We keep the prompt glyph the
	// same accent color so the transcript reads like a terminal session.
	cmd := p.Command
	if cmd == "" {
		cmd = "…"
	}
	promptStyle := lipgloss.NewStyle().Foreground(t.Info).Bold(true)
	cmdStyle := lipgloss.NewStyle().Foreground(t.Foreground).Bold(true)
	// Reserve room for `$ `, the two-space gap and the tag chip (its text
	// plus two padding cells) so the header never wraps the box border.
	tagCols := lipgloss.Width(tagStyle.Render(tag))
	cmdCols := inner - 2 /* "$ " */ - 2 /* gap */ - tagCols
	if cmdCols < 8 {
		cmdCols = 8
	}
	head := promptStyle.Render("$") + " " + cmdStyle.Render(clipCols(oneLine(cmd), cmdCols)) + "  " + tagStyle.Render(tag)

	// Elapsed / took line.
	timing := ""
	switch {
	case p.Done && p.Superseded:
		timing = s.Muted.Render("retried by the model")
	case p.Done:
		timing = s.Muted.Render(fmt.Sprintf("Took %s", humanizeDur(p.Elapsed)))
	case !p.StartedAt.IsZero():
		timing = s.Muted.Render(fmt.Sprintf("Elapsed %s", humanizeDur(time.Since(p.StartedAt))))
	default:
		timing = s.Muted.Render("• running")
	}
	if selected {
		hint := "(ctrl+o expand)"
		if p.Expanded {
			hint = "(ctrl+o preview)"
		}
		timing += "  " + s.Muted.Render(hint)
	}

	body := head + "\n" + timing
	if out := p.renderOutput(s, inner); out != "" {
		body += "\n" + out
	}
	if p.Done && !p.Superseded {
		var footer string
		switch {
		case p.TimedOut:
			footer = s.Danger.Render("Command timed out")
		case p.Failed:
			footer = s.Danger.Render(fmt.Sprintf("Command exited with code %d", p.ExitCode))
		default:
			footer = s.Success.Render("Command exited with code 0")
		}
		body += "\n" + footer
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 2).
		Render(body)
}

func (p *CommandPanel) renderOutput(s Styles, inner int) string {
	t := s.Theme
	outStyle := lipgloss.NewStyle().Foreground(t.Foreground)
	errStyle := lipgloss.NewStyle().Foreground(t.Danger)

	var lines []string
	for _, l := range splitLines(p.Stdout) {
		lines = append(lines, outStyle.Render(clipCols(l, inner)))
	}
	for _, l := range splitLines(p.Stderr) {
		lines = append(lines, errStyle.Render(clipCols(l, inner)))
	}
	if len(lines) == 0 {
		if !p.Done {
			return "" // nothing to show yet
		}
		return s.Muted.Render("(no output)")
	}
	if p.Expanded {
		return strings.Join(lines, "\n")
	}
	hidden := 0
	if len(lines) > cmdPreviewLines {
		hidden = len(lines) - cmdPreviewLines
		lines = lines[hidden:]
	}
	if hidden > 0 {
		lines = append([]string{s.Muted.Render(fmt.Sprintf("… %d earlier lines (ctrl+o to expand)", hidden))}, lines[1:]...)
	}
	return strings.Join(lines, "\n")
}

func humanizeDur(d time.Duration) string {
	// Redondeamos siempre a segundos enteros para que la línea "Elapsed …"
	// avance de forma monótona (1s → 2s → 3s) en lugar de saltar entre
	// "873ms", "1.4s" y "2s" según el intervalo entre refrescos.
	if d < time.Second {
		return "0s"
	}
	total := int(d.Round(time.Second) / time.Second)
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	m := total / 60
	s := total % 60
	if m < 60 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh%02dm", h, m)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// partialJSONNumber extracts a numeric value from a possibly-incomplete JSON.
func partialJSONNumber(raw, key string) (string, bool) {
	needle := `"` + key + `"`
	idx := strings.Index(raw, needle)
	if idx < 0 {
		return "", false
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == ':' || raw[i] == '\t') {
		i++
	}
	start := i
	for i < len(raw) {
		c := raw[i]
		if (c >= '0' && c <= '9') || c == '-' {
			i++
			continue
		}
		break
	}
	if i == start {
		return "", false
	}
	return raw[start:i], true
}
