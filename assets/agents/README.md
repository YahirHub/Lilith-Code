# Bundled subagents

Lilith embeds Claude-compatible Markdown subagents from this directory. Built-ins
have the lowest precedence: a user or project agent with the same `name` can
replace one without rebuilding the binary.

Canonical portable format:

```md
---
name: code-reviewer
description: Reviews code quality and correctness
tools: Read, Glob, Grep
model: inherit
skills: [go-development]
---

You are a code reviewer...
```

Model resolution:

- `model: default` or `model: inherit` uses the provider/model selected by the user in `/models` for the parent turn.
- A concrete model id/name or `provider/model` selects that configured model.
- Lilith additionally accepts an ordered comma-separated preference list, for example `model: claude-sonnet-4-5, gpt-5.4, default`; the first resolvable candidate is used.

The comma-separated list and `default` alias are Lilith extensions. `inherit` remains
the recommended value for agent files that must stay maximally portable to Claude Code.

Lilith also accepts common OpenCode/Pi fields and paths, but `.claude/agents`
remains the recommended format when an agent should be shareable with Claude Code.

Built-in platform specialists currently include:

- `termux-specialist`: implementation worker for Android/Termux support.
- `termux-auditor`: read-only portability and release auditor.
