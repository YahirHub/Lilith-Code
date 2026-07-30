# 072 · Forks de conversación y runtime local de plugins Claude

## Fecha

2026-07-30

## Objetivo

Continuar la compatibilidad con Claude Code después de auditar el estado real del proyecto. La respuesta anterior usada como referencia estaba desactualizada: el commit `690e753` ya había incorporado `CLAUDE.md`, commands legacy, frontmatter avanzado, memoria, hooks command/http, MCP, background agents y worktrees. Las brechas actuales verificables eran la semántica real de los forks de conversación introducidos en Claude reciente y el runtime de plugins locales dentro de skills directories.

## Forks Claude-compatible

La tool `Agent` acepta ahora:

```json
{
  "subagent_type": "fork",
  "description": "revisar alternativa",
  "prompt": "Prueba otro enfoque",
  "run_in_background": true,
  "isolation": "worktree"
}
```

Un fork:

- hereda el snapshot exacto de mensajes que el padre enviaría al modelo;
- conserva instrucciones `LILITH.md`/`CLAUDE.md`, memoria, conversación, proveedor, modelo, modo y tools activos;
- añade una sola vez la tarea delegada;
- elimina del snapshot una tool call final todavía sin resultados, evitando solicitudes incompatibles con el protocolo del proveedor;
- puede usar worktree por invocación;
- puede reanudarse con `task_id` como el resto de las sesiones hijas;
- no puede crear otro fork, aunque sí puede delegar un subagente aislado normal mientras no exceda la profundidad configurada;
- respeta `CLAUDE_CODE_FORK_SUBAGENT=0`.

Los subagentes nombrados mantienen su semántica anterior: contexto fresco y sólo el prompt delegado. Hay pruebas explícitas para impedir que el snapshot del padre se filtre accidentalmente a un agente aislado.

## Comandos

- `/subtask <tarea>` inicia por defecto un fork en background y entrega su resultado a la conversación principal.
- `/subtask --foreground <tarea>` espera el resultado como trabajo foreground.
- `/subtask --worktree <tarea>` crea el fork en un checkout Git aislado.
- `/fork` se conserva como alias de fallback de `/subtask`, equivalente al comportamiento de Claude cuando Agent View no está disponible. Lilith todavía no implementa el `/fork` moderno como una sesión independiente adjuntable en un daemon Agent View.

Las skills con `context: fork` ya no simulan el comportamiento lanzando un agente fresco: ahora usan el mismo runtime de fork y heredan la conversación real. Si la skill declara `agent`, su prompt y política se aplican sobre la rama heredada.

## Plugins locales en skills directories

Se detectan directorios con:

```text
~/.claude/skills/<plugin>/.claude-plugin/plugin.json
<proyecto>/.claude/skills/<plugin>/.claude-plugin/plugin.json
```

El proyecto debe estar marcado como confiable para cargar su plugin. El loader admite rutas por defecto o declaradas en el manifest para:

- `skills/` y `SKILL.md` en el root;
- `commands/` o archivos command individuales;
- `agents/` o archivos agent individuales;
- `hooks/hooks.json`, una ruta custom o hooks inline;
- `.mcp.json`, una ruta custom o `mcpServers` inline.

Las rutas custom se normalizan y no pueden escapar del root del plugin. Las skills custom se agregan al directorio `skills/` predeterminado; commands y agents custom reemplazan sus rutas default, siguiendo la semántica del manifest de Claude.

Los componentes se exponen como:

```text
/plugin:skill
/plugin:command
@plugin:agent
mcp__plugin_<plugin>_<server>__<tool>
```

El scanner normal de `.claude/skills` reconoce el manifest como límite de namespace para impedir que una skill de plugin aparezca duplicada sin prefijo. Las referencias locales `skills:` y `agent:` se convierten al namespace del plugin.

## Runtime portable del plugin

Se resuelven en skills, hooks y MCP:

- `${CLAUDE_PLUGIN_ROOT}`;
- `${CLAUDE_PLUGIN_DATA}`;
- `${CLAUDE_PROJECT_DIR}`;
- `${CLAUDE_SKILL_DIR}` dentro de la skill activa.

Los datos persistentes del plugin usan `~/.claude/plugins/data/<plugin>-skills-dir` y se crean con permisos restrictivos cuando un hook o servidor MCP los necesita.

Los hooks de plugin se fusionan con hooks de usuario/proyecto y acompañan a los subagentes, incluido un worktree. Se admiten handlers:

- `command`;
- `http`;
- `mcp_tool`.

`mcp_tool` acepta `server`, `tool` e `input`; resuelve expresiones como `${tool_input.file_path}` preservando objetos cuando la expresión ocupa todo el valor. Los servidores del propio plugin pueden referenciarse como `plugin:<plugin>:<server>`. Un fallo de transporte se reporta como mensaje del sistema y no bloquea el evento.

Los servidores MCP de plugin se conectan al runtime normal, heredan sus políticas de Plan Mode y se propagan a subagentes. El namespace evita colisiones con servidores de usuario/proyecto o de otros plugins.

Por seguridad, los agentes enviados por plugins ignoran `hooks`, `mcpServers` y `permissionMode`; esos componentes sólo se cargan desde el manifest confiable del plugin.

Comandos añadidos:

- `/plugins`: muestra plugins y cantidades de skills, commands, agents, hooks y MCP detectados.
- `/reload-plugins`: fuerza el rescan y reconecta MCP cuando cambia su configuración.

## Archivos principales

- `internal/plugins/plugins.go`
- `internal/plugins/plugins_test.go`
- `internal/hooks/hooks.go`
- `internal/hooks/hooks_test.go`
- `internal/mcp/config.go`
- `internal/mcp/config_test.go`
- `internal/mcp/runtime.go`
- `internal/mcp/runtime_test.go`
- `internal/subagents/runtime.go`
- `internal/subagents/helpers.go`
- `internal/tools/agent.go`
- `internal/tui/context_prepare.go`
- `internal/tui/plan_mode.go`
- `internal/tui/chat.go`
- `internal/tui/commands.go`
- `internal/tui/compat_commands.go`
- `internal/tui/plugins.go`
- `internal/tui/hooks.go`
- `internal/tui/mcp.go`
- `internal/skills/skills.go`

## Validación realizada

- `gofmt` aplicado a todos los archivos Go modificados.
- `git diff --check` sin errores.
- Con Go 1.23.2 y un módulo temporal se ejecutaron correctamente las pruebas de `internal/hooks`, `internal/mcp`, `internal/plugins`, `internal/skills`, `internal/agents` e `internal/subagents`.
- Las pruebas específicas del tool `Agent` también pasaron.
- `go vet` pasó para hooks, MCP, plugins, skills, agents y subagents en el laboratorio temporal.
- Pruebas verificadas: fork hereda conversación/modelo/tools, sanea tool calls incompletas, agente normal permanece aislado, fork recursivo falla, tool `Agent` acepta `fork`, worktree se propaga, la variable de desactivación se respeta, plugins aplican trust gate/namespace/path boundary, hooks portables expanden rutas y `mcp_tool` resuelve entradas e invoca el servidor namespaced.
- No fue posible ejecutar `go test ./...`, `go vet ./...` ni compilar la TUI en el repositorio real: `go.mod` exige Go 1.24 y el sandbox sólo tiene Go 1.23.2, sin DNS para descargar el toolchain ni los módulos Charmbracelet. Una prueba general de `internal/tools` que depende de normalización NFKC tampoco es representativa bajo el stub local de `golang.org/x/text`; las pruebas del tool `Agent` sí pasaron.

## Compatibilidad todavía pendiente

- Agent View/daemon con sesiones independientes adjuntables; por eso `/fork` usa el fallback de subtask.
- Agent Teams y mensajería peer-to-peer.
- Marketplace, instalación, actualización, dependencias, políticas corporativas y `enabledPlugins` de plugins distribuidos.
- Componentes de plugin LSP, monitors, workflows, binarios, themes y output styles.
- Hook handlers `prompt` y `agent`, porque requieren una inferencia adicional controlada por el runtime y no deben fingirse como un command hook.
