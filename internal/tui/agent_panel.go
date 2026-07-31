package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lilith/li/internal/subagents"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// AgentActivity is one tool operation performed inside a child agent. The
// parent UI keeps only a compact display record; the complete protocol
// transcript remains persisted by internal/subagents.
type AgentActivity struct {
	CallID   string
	Name     string
	Args     string
	Result   string
	Running  bool
	Failed   bool
	Started  time.Time
	Finished time.Time
}

// AgentPanel is the live transcript card for one subagent invocation. Panels
// live inside the normal chat transcript (never pinned), so scrolling upward
// moves them away exactly like reasoning/file panels.
type AgentPanel struct {
	TaskID       string
	ParentTaskID string
	Name         string
	Description  string
	Model        string
	Depth        int
	Resumed      bool
	Background   bool
	Status       string // running | completed | failed | canceled
	StartedAt    time.Time
	FinishedAt   time.Time
	Reasoning    string
	Output       string
	Activities   []AgentActivity
	Expanded     bool
}

const (
	agentRecentActivities = 5
	agentReasoningLines   = 3
	agentOutputLines      = 3
	// The complete child transcript lives in internal/subagents. Keep only a
	// bounded visual tail in the parent so a verbose worker cannot make the
	// parent TUI/session grow without bound.
	agentVisualBufferRunes = 16 * 1024
)

func newAgentPanel(e subagents.Event) *AgentPanel {
	p := &AgentPanel{Expanded: true}
	p.Apply(e)
	return p
}

func (p *AgentPanel) Apply(e subagents.Event) {
	if e.TaskID != "" {
		p.TaskID = e.TaskID
	}
	if e.ParentTaskID != "" {
		p.ParentTaskID = e.ParentTaskID
	}
	if e.AgentName != "" {
		p.Name = e.AgentName
	}
	if e.Description != "" {
		p.Description = e.Description
	}
	if e.Model != "" {
		p.Model = e.Model
	}
	if e.Depth > 0 {
		p.Depth = e.Depth
	}
	p.Resumed = p.Resumed || e.Resumed
	p.Background = p.Background || e.Background

	switch e.Kind {
	case subagents.EventStarted:
		p.Status = "running"
		if p.StartedAt.IsZero() {
			p.StartedAt = e.At
		}
	case subagents.EventThinking:
		p.Reasoning = appendAgentVisualTail(p.Reasoning, e.Content)
	case subagents.EventText:
		p.Output = appendAgentVisualTail(p.Output, e.Content)
	case subagents.EventToolStarted:
		p.startTool(e)
	case subagents.EventToolFinished:
		p.finishTool(e)
	case subagents.EventCompleted:
		p.Status = "completed"
		p.FinishedAt = e.At
		// A completion event carries the authoritative final text. Streaming may
		// already have populated Output, so avoid duplicating it.
		if strings.TrimSpace(p.Output) == "" && strings.TrimSpace(e.Content) != "" {
			p.Output = appendAgentVisualTail("", e.Content)
		}
	case subagents.EventFailed:
		p.Status = "failed"
		p.FinishedAt = e.At
		if strings.TrimSpace(e.Content) != "" {
			p.Output = appendAgentVisualTail(p.Output, "\nError: "+e.Content)
		}
	case subagents.EventCanceled:
		p.Status = "killed"
		p.FinishedAt = e.At
	}
}

func (p *AgentPanel) startTool(e subagents.Event) {
	p.Activities = append(p.Activities, AgentActivity{
		CallID: e.ToolCallID, Name: e.ToolName, Args: e.ToolArgs,
		Running: true, Started: e.At,
	})
	if len(p.Activities) > 40 {
		// Full child details remain on disk. Bound the parent transcript object so
		// a long-lived orchestrator does not grow indefinitely in memory.
		p.Activities = append([]AgentActivity(nil), p.Activities[len(p.Activities)-40:]...)
	}
}

func (p *AgentPanel) finishTool(e subagents.Event) {
	for i := len(p.Activities) - 1; i >= 0; i-- {
		a := &p.Activities[i]
		if (e.ToolCallID != "" && a.CallID == e.ToolCallID) || (e.ToolCallID == "" && a.Name == e.ToolName && a.Running) {
			a.Running = false
			a.Finished = e.At
			a.Result = compactAgentResult(e.Content)
			a.Failed = strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Content)), "error:")
			return
		}
	}
	p.Activities = append(p.Activities, AgentActivity{
		CallID: e.ToolCallID, Name: e.ToolName, Args: e.ToolArgs,
		Result: compactAgentResult(e.Content), Failed: strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Content)), "error:"),
		Started: e.At, Finished: e.At,
	})
}

func appendAgentVisualTail(base, delta string) string {
	if delta == "" {
		return base
	}
	combined := []rune(base + delta)
	if len(combined) <= agentVisualBufferRunes {
		return string(combined)
	}
	return string(combined[len(combined)-agentVisualBufferRunes:])
}

func appendAgentLine(base, line string) string {
	if strings.TrimSpace(base) == "" {
		return line
	}
	return strings.TrimRight(base, "\n") + "\n" + line
}

func compactAgentResult(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))
	if s == "" {
		return ""
	}
	line := strings.Split(s, "\n")[0]
	r := []rune(line)
	if len(r) > 120 {
		line = string(r[:120]) + "…"
	}
	return line
}

func (p *AgentPanel) toolCount() int { return len(p.Activities) }

func (p *AgentPanel) elapsed() time.Duration {
	if p.StartedAt.IsZero() {
		return 0
	}
	end := p.FinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(p.StartedAt)
}

func (p *AgentPanel) View(s Styles, width int) string {
	if width < 28 {
		width = 28
	}
	inner := width - 4
	t := s.Theme

	arrow := "▾"
	if !p.Expanded {
		arrow = "▸"
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "agent"
	}
	statusLabel := agentStatusLabel(p.Status)
	accent := t.Secondary
	if p.Status == "failed" || p.Status == "killed" {
		accent = t.Danger
	} else if p.Status == "completed" {
		accent = t.Success
	}
	depth := ""
	if p.Depth > 1 {
		depth = "d" + strconv.Itoa(p.Depth) + " · "
	}
	head := tuistyle.NewStyle().Foreground(accent).Bold(true).Render(arrow + " @" + name)
	head += s.Muted.Render("  " + depth + statusLabel + " · " + strconv.Itoa(p.toolCount()) + " tools · " + formatAgentDuration(p.elapsed()))
	if p.Resumed {
		head += s.Muted.Render(" · reanudado")
	}
	if p.Background {
		head += s.Muted.Render(" · background")
	}

	body := head
	if p.Expanded {
		if d := strings.TrimSpace(p.Description); d != "" {
			body += "\n" + s.Muted.Render(trimRunes(d, inner))
		}
		meta := "id: " + p.TaskID
		if model := strings.TrimSpace(p.Model); model != "" {
			meta += " · modelo: " + model
		}
		if p.ParentTaskID != "" {
			meta += " · padre: " + p.ParentTaskID
		}
		if strings.TrimSpace(meta) != "id:" {
			body += "\n" + s.Muted.Render(trimRunes(meta, inner))
		}
		if thinking := agentTail(p.Reasoning, inner, agentReasoningLines); len(thinking) > 0 {
			body += "\n" + tuistyle.NewStyle().Foreground(t.Muted).Italic(true).Render("pensando: "+thinking[0])
			for _, line := range thinking[1:] {
				body += "\n" + tuistyle.NewStyle().Foreground(t.Muted).Italic(true).Render("          "+line)
			}
		}
		if len(p.Activities) > 0 {
			start := len(p.Activities) - agentRecentActivities
			if start < 0 {
				start = 0
			}
			if start > 0 {
				body += "\n" + s.Muted.Render("… "+strconv.Itoa(start)+" herramientas anteriores")
			}
			for _, a := range p.Activities[start:] {
				body += "\n" + renderAgentActivity(s, a, inner)
			}
		}
		if output := agentTail(p.Output, inner, agentOutputLines); len(output) > 0 {
			label := "respuesta: "
			if p.Status == "running" {
				label = "avance: "
			}
			body += "\n" + tuistyle.NewStyle().Foreground(t.Foreground).Render(label+output[0])
			for _, line := range output[1:] {
				body += "\n" + tuistyle.NewStyle().Foreground(t.Foreground).Render(strings.Repeat(" ", len([]rune(label)))+line)
			}
		}
	}

	return tuistyle.NewStyle().
		Border(tuistyle.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(width - 2).
		Render(body)
}

func renderAgentActivity(s Styles, a AgentActivity, width int) string {
	state := "·"
	style := s.Muted
	if a.Running {
		state = "›"
		style = tuistyle.NewStyle().Foreground(s.Theme.Secondary)
	} else if a.Failed {
		state = "x"
		style = tuistyle.NewStyle().Foreground(s.Theme.Danger)
	} else {
		state = "+"
		style = tuistyle.NewStyle().Foreground(s.Theme.Success)
	}
	args := prettyToolArgs(a.Args)
	line := state + " " + a.Name
	if args != "" {
		line += "  " + args
	}
	if !a.Running && a.Result != "" && a.Failed {
		line += "  " + a.Result
	}
	return style.Render(trimRunes(line, width))
}

func agentTail(content string, width, maxLines int) []string {
	lines := wrapThinking(strings.TrimSpace(content), width)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func agentStatusLabel(status string) string {
	switch status {
	case "completed":
		return "completado"
	case "failed":
		return "falló"
	case "killed":
		return "cancelado"
	default:
		return "ejecutando"
	}
}

func formatAgentDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func trimRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(strings.ReplaceAll(strings.TrimSpace(s), "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
