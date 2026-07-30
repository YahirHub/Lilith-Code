# 069 · Compatibilidad con el ecosistema Claude

## Objetivo

Permitir que un repositorio preparado para Claude Code pueda reutilizar en Lilith sus instrucciones, skills, comandos legacy, subagentes, memoria, hooks, MCP y aislamiento por worktree sin mantener configuraciones paralelas.

## Instrucciones persistentes

Lilith mantiene como formato nativo `LILITH.md`/`LI.md` y, cuando la compatibilidad Claude está habilitada, carga también:

- `~/.claude/CLAUDE.md`.
- `CLAUDE.md`, `.claude/CLAUDE.md` y `CLAUDE.local.md` en la jerarquía del proyecto.
- `.claude/rules/**/*.md`, incluido `paths:`.
- imports `@archivo` con protección de ciclos y hasta cinco saltos, como Claude; imports externos de instrucciones de proyecto sólo se expanden cuando el workspace está marcado como confiable.
- `claudeMdExcludes`.
- `CLAUDE.md` anidados bajo el cwd de forma lazy al tocar archivos de esos directorios.

Los comentarios HTML de bloque se eliminan del contexto inyectado. La UI de `/config > General` permite apagar por separado instrucciones Lilith, compatibilidad Claude, memoria y hooks.

## Skills y comandos

Se conservan Agent Skills (`SKILL.md`) como formato compartido. Además se interpretan metadatos Claude relevantes: invocación manual/modelo, `allowed-tools`, `disallowed-tools`, `context: fork`, agent, background, model, effort, hooks, shell dinámico y `skillOverrides` desde settings. `.claude/commands/*.md` se incorpora como compatibilidad legacy sin crear un segundo runtime de comandos.

## Memoria

La conversación principal usa la ubicación Claude-compatible `~/.claude/projects/<repo>/memory/` cuando la compatibilidad está activa. Los worktrees del mismo repositorio comparten memoria. Se respeta `autoMemoryDirectory` y `CLAUDE_CODE_DISABLE_AUTO_MEMORY`; overrides de proyecto sólo se aceptan con workspace confiable.

Los subagentes admiten `memory: user|project|local` usando `.claude/agent-memory` y `.claude/agent-memory-local`. El arranque carga como máximo 200 líneas o 25 KiB de `MEMORY.md`.

## Hooks, MCP y confianza

Se soportan hooks de usuario/proyecto y hooks declarados por skills/subagentes para los eventos principales de tools, sesiones y subagentes. Los hooks ejecutables del proyecto y MCP inline del proyecto sólo se habilitan cuando `/config > Seguridad > Proyecto confiable` está activo.

MCP soporta stdio, Streamable HTTP, SSE legacy y WebSocket. Los subagentes heredan MCP del padre y pueden añadir servidores inline propios. Plan Mode sólo expone MCP marcados read-only. Los hooks `WorktreeCreate`/`WorktreeRemove` participan en el ciclo de aislamiento: `WorktreeCreate` reemplaza la creación git predeterminada y debe devolver una ruta absoluta; `WorktreeRemove` permite limpiar checkouts personalizados.

## Subagentes

`.claude/agents/**/*.md` es el formato canónico. Se conservan contexto aislado, tools/modelo/skills propios, paralelismo, resume por `task_id`, background, memoria, MCP, hooks y `isolation: worktree`. Lilith mantiene además nesting controlado como extensión de orquestación propia; las versiones actuales de Claude Code pueden ser más restrictivas con subagentes nombrados, por lo que un agente portable no debe depender obligatoriamente del nesting. `.worktreeinclude` copia únicamente archivos gitignored compatibles con sus patrones y el shell del hijo no puede redirigirse al checkout principal mediante `git -C`/`--git-dir`/variables Git equivalentes. Se respetan también `worktree.baseRef`, `sparsePaths` y `symlinkDirectories` de settings confiables.

## Compatibilidad deliberadamente no fingida

Lilith no pretende ejecutar plugins binarios de Claude Code ni reproducir servicios propietarios/enterprise. La compatibilidad de hooks se centra en handlers `command`/`http`; los backends propietarios o dependientes del runtime Claude (`prompt`, `agent`, `mcp_tool` como hook) no se anuncian como equivalencia 1:1. Tampoco se replica el sistema experimental de Agent Teams ni el marketplace de plugins. Campos o políticas que requieran infraestructura específica de Anthropic deben fallar de forma explícita o degradarse de forma segura; nunca deben ampliar permisos silenciosamente.
