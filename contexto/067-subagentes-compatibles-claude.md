# 067 - Subagentes compatibles con Claude

## Objetivo

Añadir subagentes aislados sin inventar otro formato de configuración. El
formato canónico de Lilith es el Markdown de Claude Code: frontmatter YAML +
cuerpo Markdown como system prompt. El mismo archivo debe poder compartirse
entre herramientas siempre que use campos comunes.

## Decisiones de arquitectura

- Una única tool estable `Agent` delega trabajo; `Task`, `task` y `agent` se
  aceptan como aliases de runtime por compatibilidad histórica.
- El padre sólo recibe el resultado final del subagente. El historial completo,
  tool calls y razonamiento del hijo permanecen en su sesión separada.
- Si el modelo emite varias llamadas `Agent` independientes en el mismo lote,
  Lilith las ejecuta en paralelo y conserva el orden de los resultados.
- El hijo empieza con contexto nuevo: prompt del agente + tarea delegada +
  entorno + skills precargadas. No copia el historial del padre.
- Las sesiones hijas se persisten bajo
  `~/.li/projects/<proyecto>/subagents/<task_id>.json` y pueden continuarse
  pasando `task_id` a `Agent`.
- No hay límite global artificial de tool calls. `maxTurns` sólo se aplica si el
  propio archivo del agente lo declara explícitamente.
- Los subagentes pueden delegar a otros subagentes igual que Claude Code. La
  profundidad por defecto es 3 niveles bajo el hilo principal; puede ajustarse
  con `LILITH_MAX_SUBAGENT_DEPTH` o `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`.
  Al llegar al límite, la tool `Agent` deja de estar disponible en ese hijo.
- Si el padre está en Plan, cualquier subagente queda forzado a política de
  sólo lectura, aunque su definición normal permita edición. Plan no puede
  usarse para escapar de sus restricciones.
- `permission: ask` de OpenCode se trata como deny dentro del hijo: no existe un
  prompt de permisos interactivo separado dentro de un subagente aislado y es
  preferible fallar cerrado.

## Descubrimiento y precedencia

De menor a mayor prioridad:

1. `assets/agents/` embebidos.
2. Rutas globales compatibles con Pi/OpenCode/OpenClaude/Agent/Claude.
3. `~/.li/agents`.
4. Rutas de proyecto compatibles, buscando desde la raíz Git hacia el cwd.
5. `<proyecto>/.li/agents`.

Rutas compatibles relevantes:

- `~/.claude/agents`, `.claude/agents`
- `~/.agents/agents`, `.agents/agents`
- `~/.pi/agent/agents`, `~/.pi/agents`, `.pi/agents`
- `~/.config/opencode/agents`, `.opencode/agents`
- `~/.openclaude/agents`, `.openclaude/agents`
- `~/.li/agents`, `.li/agents`

La identidad usa `name:` como Claude. Para un archivo OpenCode sin `name`, se
usa el nombre base del `.md`.

## Formato recomendado

```md
---
name: code-reviewer
description: Reviews code for correctness, security and maintainability
tools: Read, Glob, Grep
model: inherit
skills: [go-development]
maxTurns: 12
---

You are a focused code reviewer...
```

Campos Claude implementados:

- `name`
- `description`
- `tools`
- `disallowedTools`
- `model`
- `permissionMode` (`plan` tiene semántica real)
- `maxTurns`
- `skills`
- `hidden` (metadata/visibilidad de `/agents`)

Campos leídos pero todavía sin runtime equivalente completo:

- `background`: actualmente la ejecución es foreground/síncrona.
- `isolation: worktree`: se reconoce, pero la ejecución se rechaza explícitamente hasta implementar worktrees reales; nunca se ignora silenciosamente.
- `color`: metadata, sin color de hijo independiente en la TUI.

No se implementan todavía hooks, MCP por agente, memory ni effort. No deben
simularse silenciosamente como si existieran.

## Compatibilidad de tools

Mapeo principal Claude/OpenCode -> Lilith:

- `Read` -> `read_files`
- `Glob` -> `glob`
- `Grep` -> `code_search`
- `List` -> `list_directory`
- `Bash` / `PowerShell` -> `run_terminal_command`
- `Edit` -> `str_replace`, `apply_diff`
- `Write` -> `create_file`, `str_replace`, `apply_diff`
- `WebFetch` -> `read_url`
- `WebSearch` -> `web_search`
- `TodoWrite` -> `todo_write`
- `Skill` -> tools nativas de skills
- `ToolSearch` -> `tool_search`

Los nombres nativos de Lilith también se aceptan directamente.

## Modelo

Orden práctico:

1. override `model` de la llamada `Agent`;
2. `model:` del archivo;
3. modelo del padre.

`default` e `inherit` heredan el proveedor/modelo seleccionado por el usuario en
`/models` para el turno padre. `provider/model` selecciona un modelo configurado
de ese provider. IDs exactos se buscan entre providers configurados. Los aliases
Claude `sonnet`, `opus`, `haiku` se resuelven cuando existe un modelo compatible.

Como extensión de Lilith, `model` también acepta una lista ordenada separada por
comas, por ejemplo `model: claude-sonnet-4-5, gpt-5.4, default`. Se usa el primer
candidato que pueda resolverse contra los providers/modelos configurados. Si una
lista termina en `default`, el modelo seleccionado en `/models` actúa como
fallback explícito. La misma sintaxis se acepta en el override `model` de la tool
`Agent`.

## Uso

Delegación automática: el prompt principal recibe únicamente
`<available_agents>` con `name + description`. El modelo llama `Agent` cuando la
descripción coincide con una tarea autocontenida.

Delegación manual estilo OpenCode:

```text
@Explore encuentra dónde se construyen los schemas de tools
@code-reviewer revisa los cambios de autenticación
```

`/agents` lista los subagentes visibles detectados.

## Agentes embebidos

- `Explore`: exploración rápida de sólo lectura.
- `Plan`: investigación/planificación de sólo lectura.
- `general-purpose`: trabajador aislado de propósito general.

Cualquier agente de usuario/proyecto con el mismo `name` reemplaza al embebido.

## Validación

Se añadieron pruebas para:

- parseo Claude;
- fallback de nombre OpenCode;
- precedencia builtin < user < project;
- permisos OpenCode básicos y `tools` boolean legacy;
- aliases `Task -> Agent`;
- contexto aislado del hijo;
- nesting de subagentes y profundidad configurable;
- restricción read-only heredada desde Plan;
- mapeo de tools Claude.
