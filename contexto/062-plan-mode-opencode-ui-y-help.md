# 062 · Plan Mode inspirado en OpenCode, UI compacta y /help

## Referencia analizada

Se revisó el código fuente adjunto de OpenCode (`opencode-dev.zip`), en particular:

- `packages/tui/src/config/keybind.ts`: `Tab` / `Shift+Tab` ejecutan el ciclo del agente primario.
- `packages/tui/src/context/local.tsx`: el agente seleccionado cambia para el siguiente prompt; el turno ya creado conserva su agente.
- `packages/opencode/src/agent/agent.ts`: `build` y `plan` son agentes primarios con políticas de permisos distintas.
- `packages/opencode/src/session/prompt/plan-mode.txt`: flujo de exploración, aclaración, diseño, revisión y salida del plan.
- `packages/opencode/src/session/prompt/build-switch.txt`: recordatorio explícito al pasar de Plan a Build.

Lilith replica el comportamiento, no el runtime TS de OpenCode.

## Build / Plan como modos de primera clase

- `Tab` y `Shift+Tab` alternan Build / Plan.
- El modo seleccionado pertenece al **siguiente turno**.
- Al iniciar un turno se captura `turnAgentMode`; pulsar Tab durante streaming no cambia los permisos del turno actual.
- El modo se persiste dentro de la sesión y del live checkpoint.

## Política de Plan Mode

Plan Mode no depende sólo del prompt:

- herramientas mutantes quedan fuera de schemas y de `tool_search`;
- `todo_write` queda oculto durante planificación;
- `run_terminal_command` usa una allowlist estricta de inspección;
- comandos de shell directos con `!` pasan por la misma restricción;
- la política se vuelve a comprobar inmediatamente antes de ejecutar una tool;
- `plan_question` permite detenerse ante 1–3 decisiones materiales;
- `plan_exit` entrega el plan final y termina el turno.

`plan_exit` debe ser la única acción final de su respuesta. Un plan listo no cambia automáticamente a Build: el usuario conserva el control mediante Tab o `/build`.

Si el usuario ya seleccionó Build mientras el turno Plan seguía ejecutándose, el turno conserva Plan y puede terminar correctamente; el plan queda programado como handoff para el siguiente turno Build.

## Transición a Build

Al pasar de Plan a Build después de completar un plan:

1. se conserva el plan aprobado;
2. el siguiente turno Build consume el handoff una sola vez;
3. el system prompt recibe un recordatorio explícito de que las restricciones read-only terminaron;
4. el plan aprobado se inserta como contexto de implementación.

## Interfaz

La barra inferior queda reducida a:

- directorio de trabajo;
- proveedor en color secundario;
- modelo en color primario;
- barra de contexto y `usado/total` con números sin abreviaturas, porcentaje ni texto adicional.

No se muestran permanentemente ayudas de teclas ni comandos en el status bar.

En Plan:

- el borde/prompt del editor usa el color secundario;
- las preguntas pendientes y el estado de plan listo aparecen encima del input;
- TodoWrite se oculta mientras el turno efectivo es Plan.

## /help

`/help` abre una pantalla independiente con scroll y documenta:

- todos los slash commands y aliases;
- Build / Plan;
- envío, steering y follow-up;
- nueva línea;
- cancelación y recuperación de cola;
- navegación del transcript;
- paneles y razonamiento;
- shell directo y skills;
- `/exit` como salida explícita.

## Persistencia

`Session` y `LiveCheckpoint` incorporan `Plan`. Un cambio de modo con Tab durante un turno se guarda en el checkpoint live para no promover un stream parcial a snapshot estable.
