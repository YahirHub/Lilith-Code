# 019 — Codex: reintento de tool call deja panel huérfano "escribiendo…"

## Síntoma
Durante una edición (`str_replace`) la Responses API de Codex a veces se
atasca a mitad del streaming de argumentos. El backend lo reintenta emitiendo
un **nuevo** `response.output_item.added` de tipo `function_call` con otro
`output_index` (y otro `item_id` / `call_id`), pero **nunca cierra el anterior
con `response.output_item.done`**. En la TUI se veían dos paneles:

- el primero, con la diff a medias, congelado en `escribiendo…`
- el segundo, con la nueva ejecución, avanzando normalmente

El "trabajando…" tampoco reflejaba el estado real: el panel huérfano tenía
el shimmer, pero el turno estaba en otra tool call.

## Causa
`parseCodexSSE` mantenía en `pending`/`order` cada `output_index` que veía y
no distinguía cuáles habían recibido `output_item.done`. El TUI creaba un
`FilePanel` por Index y lo dejaba abierto hasta el `Finish` que llegaba con
el resultado real de la herramienta — pero para el panel huérfano ese
resultado nunca llega (la tool call abandonada no se ejecuta).

Con `parallel_tool_calls=false` es imposible que dos function_calls estén
legítimamente en vuelo al mismo tiempo, así que cualquier `output_item.added`
que aparezca mientras hay pendings sin `.done` es un reintento server-side.

## Fix
### Transporte (`internal/providers/openai/codex_transport.go`)
- Nuevo set `doneIdx map[int]bool` que se marca en `response.output_item.done`.
- En `response.output_item.added`, si aparece un `output_index` que no existía
  y hay otros pending sin `.done`, se sacan de `order`, se marcan en `doneIdx`
  y se propagan a la TUI vía el nuevo campo `Chunk.SupersededIndices []int`.
- El snapshot que crea el panel del nuevo tool call viaja en el mismo chunk
  que la lista `superseded`, así el usuario ve el panel viejo colapsarse y el
  nuevo empezar a "escribir" en el mismo frame.

### TUI (`internal/tui/chat.go`, `internal/tui/filepanel.go`)
- `FilePanel.Superseded bool` y `FilePanel.MarkSuperseded()` que fija
  `Done=true`, `Expanded=false` y título "Reintentado <path>".
- `chatStreamMsg.superseded []int`: cuando llega, cierra los `livePanels[idx]`
  correspondientes antes de aplicar el snapshot nuevo.
- Red de seguridad en `applyToolCalls`: al crear un panel para un `Index`
  nuevo, cualquier panel previo sin `Done` se marca automáticamente como
  Superseded. Cubre también reintentos que no vengan por SSE (p. ej. si el
  cliente HTTP reabriera la conexión en el futuro).

## Verificación
- `go build ./...` ok.
- `go test ./...` ok.

## Commit sugerido
- **subject**: `fix(codex): colapsar paneles huérfanos cuando el backend reintenta un tool call`
- **description**: parseCodexSSE detecta cuando un `response.output_item.added` de tipo function_call llega mientras hay pendings previos sin `output_item.done` (reintento server-side de Codex al atascarse un `str_replace`). Emite `Chunk.SupersededIndices` con los output_index abandonados; la TUI marca esos `FilePanel` como `Superseded` (colapsados, título "Reintentado <path>") en vez de dejarlos en "escribiendo…" para siempre. Añade además red de seguridad en `applyToolCalls` para cubrir cualquier otra ruta que cree un panel nuevo sin haber cerrado el anterior.
