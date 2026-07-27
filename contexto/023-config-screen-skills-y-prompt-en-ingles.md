# 022 · /config screen, Claude-style skills, English prompt & tools

## Summary
Add a dedicated `/config` TUI screen, a Claude Code–style skills engine (`~/.li/skills` and `./.li/skills`), `/skills:<name>` invocation with palette autocompletion, and translate the system prompt + tool schemas to English (comments stay in Spanish).

## Description
- `internal/config/config.go`: new `SkillsEnabled` setting (persisted in `settings.json`).
- `internal/skills/skills.go` (new): loads SKILL.md files with YAML frontmatter (name/description), inspired by pi.dev's `packages/coding-agent/src/core/skills.ts`. Exposes `Load`, `Find`, `Filter`, `ReadContent`, `FormatForPrompt`, `UserDir`, `ProjectDir`.
- `internal/tui/config_screen.go` (new): `/config` opens a real screen with a "Claude Code skills" toggle, config path, detected skills and back-to-chat action.
- `internal/tui/commands.go`: `/config` now routes to `NewConfigScreen(ctx)` instead of dumping a text line.
- `internal/tui/chat.go`:
  - System prompt rewritten to English with pi-inspired hard rules against lazy placeholders (`// rest of code`, `...`, `<!-- resto -->`) and explicit "produce ALL requested artifacts in the same turn" rule for the EN + ES page regression.
  - `skillsBlock()` appends the `<available_skills>` XML block when skills are enabled.
  - Palette now allows `:` and merges skill entries; typing `/foo` shows matching skills as `skills:foo`, and pressing Enter invokes them.
  - `submit()` intercepts `/skills:<name> [args]` and injects the SKILL.md body as a user message before running the turn.
- `internal/tools/{files,exec,web,registry}.go`: all `Description` fields and JSON-schema `"description"` fields translated to English so the model reasons in its stronger language. Runtime error strings kept in Spanish (user-facing).

## Commit
```
feat(tui): /config screen, Claude Code skills and English prompt/tools

Adds a dedicated /config screen with a Skills toggle (persisted in
settings.json), a skills engine that loads SKILL.md from ~/.li/skills and
./.li/skills (Claude Code / pi.dev format) and exposes them via
/skills:<name> with palette autocompletion. Rewrites the system prompt in
English with pi-inspired anti-laziness rules (never emit partial files or
placeholders; deliver all requested artifacts in the same turn) and
translates every tool description and JSON-schema description to English
so the model uses its stronger reasoning language.
```
