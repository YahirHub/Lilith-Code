# Completado 14 · Forks y runtime local de plugins Claude

## Resultado

- Fork real de la conversación mediante `Agent(subagent_type: fork)`.
- `/subtask` y fallback `/fork`, con foreground/background y worktree.
- `context: fork` corregido para heredar historial en vez de crear un agente fresco.
- Plugins locales de skills directories con manifest, namespace, path boundary y trust gate.
- Skills, commands, agentes, hooks y servidores MCP de plugin con hot reload.
- Variables portables `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`, `CLAUDE_PROJECT_DIR` y `CLAUDE_SKILL_DIR`.
- Hook handler `mcp_tool` con sustitución declarativa de input y servidor namespaced.
- Restricciones de seguridad para agentes de plugin.
- Pruebas de regresión y documentación en `contexto/072-forks-y-plugins-locales-claude.md`.
