# 068 - Orquestación visible de subagentes

## Objetivo

Convertir `Agent` de una ejecución aislada pero opaca a un runtime observable:
el agente principal puede delegar varias unidades independientes en paralelo,
seguir cada trabajador dentro del transcript y sintetizar sus resultados sin
inyectar el historial interno de los hijos al contexto principal.

El formato de definición sigue siendo el Markdown compatible con Claude
(`.claude/agents/**/*.md`). Este cambio modifica el runtime y la presentación,
no crea un segundo formato.

## Estado de tarea

Cada sesión hija persiste metadata equivalente al task state usado por
OpenClaude:

- `running`
- `completed`
- `failed`
- `killed`

Además conserva `task_id`, padre, profundidad, descripción, provider/modelo,
fechas, tools materializadas y el historial protocol-correcto completo. El
`task_id` continúa siendo el identificador para reanudar el mismo contexto.

## Eventos de progreso

`internal/subagents` emite un stream desacoplado de la TUI:

- `started`
- `thinking`
- `text`
- `tool_started`
- `tool_finished`
- `completed`
- `failed`
- `canceled`

La callback nunca modifica el `ChatModel`. El host coloca los eventos en un
canal y Bubble Tea los consume en su propio `Update`, evitando data races.

## UI

Cada invocación materializa un `AgentPanel` dentro del transcript normal.
No es un widget anclado al footer: al hacer scroll se desplaza junto con el
resto de la conversación.

El panel muestra de forma compacta:

- agente, profundidad, estado y duración;
- modelo;
- descripción delegada;
- tail del reasoning summary emitido por el proveedor;
- hasta las cinco actividades recientes, siguiendo el patrón de progreso de
  Claude/OpenClaude;
- tail del texto que el hijo está produciendo.

`Ctrl+G` expande/pliega el panel del subagente más reciente. Los resultados
completos del hijo no se copian al transcript del padre: permanecen en su
sesión hija.

## Paralelismo y orquestación

Si una respuesta contiene sólo varias llamadas `Agent`, el runtime del padre
las ejecuta concurrentemente y conserva el orden protocolar de los resultados.
El mismo comportamiento existe dentro de un subagente, por lo que un worker
que todavía esté bajo el límite de profundidad puede orquestar otro lote.

El prompt/tool metadata indica explícitamente al modelo que emita varias
llamadas `Agent` en el mismo response cuando el trabajo sea independiente y
que sintetice sus resultados al volver.

## Nesting

Lilith mantiene el límite configurable implementado en 067 (3 por defecto):

- `LILITH_MAX_SUBAGENT_DEPTH`
- `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`

Al llegar al límite, `Agent` desaparece de las tools del hijo. Los hijos que sí
pueden delegar reciben `<available_agents>` dentro de su propio system prompt,
no sólo el agente principal.

Este comportamiento sigue la semántica actual de Claude Code: por defecto los
subagentes pueden crear otros subagentes hasta tres capas por debajo de la
conversación principal y, al llegar al límite, la herramienta `Agent` deja de
estar disponible. `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH` se acepta directamente
para conservar la misma configuración.

## Cancelación

Todo el árbol hereda el `context.Context` del turno principal. `Esc` cancela el
padre y propaga la señal a cada worker y descendiente. Los paneles reciben
estado terminal `killed/cancelado` en lugar de quedar eternamente en running.

## Persistencia

El `TranscriptEntry` del padre guarda sólo la proyección visual del panel para
restaurar `/history`/`--continue`. El historial completo de cada hijo sigue en:

`~/.li/projects/<proyecto>/subagents/<task_id>.json`

Así el padre puede mostrar el proceso sin pagar ese historial interno como
contexto del LLM principal.

## Optimización del streaming visual

Los deltas de reasoning/texto de varios workers pueden llegar con mucha
frecuencia. La TUI no redibuja el transcript por cada token: `agentEventPump`
agrupa eventos durante una ventana corta (~35 ms, hasta 128 eventos) y Bubble
Tea aplica el lote en un único frame. Esto evita que la observabilidad de
subagentes vuelva lento el chat principal cuando trabajan varios a la vez.

La proyección visual del padre conserva como máximo 40 actividades y un tail
acotado de reasoning/salida. No se pierde información del hijo: el transcript
protocolar completo sigue en el archivo de la sesión hija. El panel expandido
muestra las cinco actividades recientes, equivalente al enfoque de progreso
reciente de OpenClaude, para seguir siendo usable en terminales pequeñas.

## Diferencia de presentación respecto a Claude Code

Claude Code actual mantiene un panel de subagentes debajo del prompt y permite
abrir el árbol de hijos. Lilith conserva la misma relación padre/hijo mediante
`task_id`, `parentTaskId` y `depth`, pero presenta cada ejecución como un bloque
dentro del transcript. Es deliberado: respeta la regla de Lilith de no dejar
widgets anclados cuando el usuario hace scroll y funciona mejor en pantallas
pequeñas. Cada bloque muestra su `task_id`; los hijos muestran además el padre.
