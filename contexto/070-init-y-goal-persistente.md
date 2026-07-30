# 070 · `/init` y `/goal` persistente

## `/init`

`/init` inicia un turno Build dedicado que inspecciona el repositorio real y crea o mejora `LILITH.md` en la raíz. No genera una plantilla ciega y no crea `CLAUDE.md`.

Como fuentes de evidencia revisa README/manifiestos/build/CI/código representativo e instrucciones existentes de Claude/AGENTS, Cursor, Copilot, Devin, Windsurf y Cline. El resultado debe ser conciso (objetivo menor a 200 líneas), durable y verificable contra el repositorio.

## `/goal`

`/goal` mantiene un único objetivo durable por sesión, separado del texto del chat. Estados:

- `active`
- `paused`
- `blocked`
- `usage_limited`
- `budget_limited`
- `complete`

Uso:

- `/goal [--tokens N] <objetivo>`
- `/goal status`
- `/goal pause`
- `/goal resume`
- `/goal complete`
- `/goal clear`

El goal persiste en Session/LiveCheckpoint y se reanuda después de restaurar una sesión. Mientras está activo, Lilith encadena continuaciones autónomas en fronteras seguras hasta completar, bloquearse o alcanzar presupuesto/límite duro de uso. Los límites transitorios de rate no se confunden con `usage_limited`. El protocolo actual de Codex modela un único goal durable por thread mediante set/get/clear; Lilith mantiene esa semántica y añade la posibilidad de reorientar de forma segura un turno ya activo mediante steering en vez de abrir un segundo turno paralelo.

Las tools `create_goal`, `get_goal` y `update_goal` permiten que el propio agente mantenga el estado. El contador incluye contexto aproximado y salida generada para que `token_budget` pueda detener la ejecución de manera durable.
