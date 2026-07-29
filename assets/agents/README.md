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

Lilith also accepts common OpenCode/Pi fields and paths, but `.claude/agents`
remains the recommended format when an agent should be shareable with Claude Code.
