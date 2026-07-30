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

Los plugins locales guardados en `~/.claude/skills/<plugin>` o `.claude/skills/<plugin>` con `.claude-plugin/plugin.json` aportan skills, commands, agents, hooks y servidores MCP. Skills, commands y agents usan namespace `plugin:nombre`; MCP se expone como `mcp__plugin_<plugin>_<server>__<tool>`. Los plugins del proyecto sólo se cargan cuando el workspace es confiable; sus agentes ignoran `hooks`, `mcpServers` y `permissionMode`, igual que Claude por seguridad. Las rutas declaradas en el manifest no pueden escapar del root del plugin y se resuelven `${CLAUDE_PLUGIN_ROOT}`, `${CLAUDE_PLUGIN_DATA}` y `${CLAUDE_PROJECT_DIR}`.

## Memoria

La conversación principal usa la ubicación Claude-compatible `~/.claude/projects/<repo>/memory/` cuando la compatibilidad está activa. Los worktrees del mismo repositorio comparten memoria. Se respeta `autoMemoryDirectory` y `CLAUDE_CODE_DISABLE_AUTO_MEMORY`; overrides de proyecto sólo se aceptan con workspace confiable.

Los subagentes admiten `memory: user|project|local` usando `.claude/agent-memory` y `.claude/agent-memory-local`. El arranque carga como máximo 200 líneas o 25 KiB de `MEMORY.md`.

## Hooks, MCP y confianza

Se soportan hooks de usuario/proyecto, plugins y hooks declarados por skills/subagentes para los eventos principales de tools, sesiones y subagentes. Los handlers portables implementados son `command`, `http` y `mcp_tool`; este último resuelve entradas declarativas desde el payload del evento e invoca un tool MCP namespaced sin bloquear el evento ante fallos de transporte. Los hooks ejecutables del proyecto y MCP inline del proyecto sólo se habilitan cuando `/config > Seguridad > Proyecto confiable` está activo.

MCP soporta stdio, Streamable HTTP, SSE legacy y WebSocket. Los subagentes heredan MCP del padre y pueden añadir servidores inline propios. Plan Mode sólo expone MCP marcados read-only. Los hooks `WorktreeCreate`/`WorktreeRemove` participan en el ciclo de aislamiento: `WorktreeCreate` reemplaza la creación git predeterminada y debe devolver una ruta absoluta; `WorktreeRemove` permite limpiar checkouts personalizados.

## Subagentes

`.claude/agents/**/*.md` es el formato canónico. Se conservan contexto aislado, tools/modelo/skills propios, paralelismo, resume por `task_id`, background, memoria, MCP, hooks y `isolation: worktree`. Lilith mantiene además nesting controlado como extensión de orquestación propia; las versiones actuales de Claude Code pueden ser más restrictivas con subagentes nombrados, por lo que un agente portable no debe depender obligatoriamente del nesting. `.worktreeinclude` copia únicamente archivos gitignored compatibles con sus patrones y el shell del hijo no puede redirigirse al checkout principal mediante `git -C`/`--git-dir`/variables Git equivalentes. Se respetan también `worktree.baseRef`, `sparsePaths` y `symlinkDirectories` de settings confiables.

## Compatibilidad deliberadamente no fingida

Lilith no pretende ejecutar plugins binarios de Claude Code ni reproducir servicios propietarios/enterprise. El loader de plugins se limita por ahora al formato local en skills directories y a sus componentes portables `skills`, `commands`, `agents`, `hooks` y `mcpServers`; no instala marketplaces ni ejecuta todavía LSP, monitors, workflows, binarios declarados, temas u output styles suministrados por un plugin. La compatibilidad de hooks cubre handlers `command`, `http` y `mcp_tool`; los backends que requieren una inferencia adicional del runtime Claude (`prompt` y `agent`) no se anuncian como equivalencia 1:1. Tampoco se replica el sistema experimental de Agent Teams ni Agent View como daemon multiproceso. Campos o políticas que requieran infraestructura específica de Anthropic deben fallar de forma explícita o degradarse de forma segura; nunca deben ampliar permisos silenciosamente.
