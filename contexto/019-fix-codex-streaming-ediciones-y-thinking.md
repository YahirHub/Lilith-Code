---
name: Fix streaming Codex de ediciones y del shimmer "Pensando"
description: 018 — Indexar tool calls por output_index y mantener el indicador visible mientras no haya panel
type: feature
---
# 018 · Fix streaming de ediciones (str_replace) y desaparición del "Pensando…"

## Problemas reportados

1. Con Codex (Responses API), las herramientas de **edición** (`str_replace`)
   volvían a aparecer sólo al final del turno. Las de creación (`write_file`)
   con args pequeños llegaban en vivo, pero las ediciones (JSON grande, casi
   siempre por deltas) no.
2. Cuando el modelo no devolvía absolutamente nada durante unos segundos, el
   indicador **"Pensando…"** desaparecía y el chat quedaba en blanco.

## Causa 1 — Clave errónea en `parseCodexSSE`

`internal/providers/openai/codex_transport.go` indexaba las tool calls
pendientes por `call_id`:

- `response.output_item.added` trae `item.call_id` (`call_…`) e `item.id`
  (`fc_…`) → se guardaba bajo `call_id`.
- `response.function_call_arguments.delta` trae `item_id` (`fc_…`) y **no**
  trae `call_id` → se creaba un pending nuevo sin `name`, y
  `snapshotCodex` lo descartaba por nombre vacío.
- Resultado: los deltas se perdían, y sólo el `output_item.done` final
  (que sí trae `call_id`) rellenaba los argumentos → la ventana de edición
  aparecía de golpe al terminar.

`pi/packages/ai/src/api/openai-responses-shared.ts` confirma que la clave
correcta es `output_index`, que sí viaja en todos los eventos
(`output_item.added`, `function_call_arguments.delta`,
`function_call_arguments.done`, `output_item.done`).

### Solución

- `pending` pasa a ser `map[int]*ToolCall` indexado por `output_index`.
- Se mantienen dos alias defensivos (`byItemID`, `byCallID`) para tolerar
  eventos que sólo traen uno de los identificadores.
- `resolveIdx(outputIdx, itemID, callID)` centraliza el lookup y crea el
  slot al vuelo cuando el evento llega antes del `output_item.added`.
- Se añade el caso `response.function_call_arguments.done`: fija el JSON
  final antes del `output_item.done` (evita perder el último delta si el
  backend lo cierra sólo con `arguments`).
- `snapshotCodex` ahora recibe la nueva firma `map[int] + []int`.

## Causa 2 — `m.thinking = false` en snapshots parciales

`internal/tui/chat.go` apagaba el shimmer en cuanto llegaba **cualquier**
`chatStreamMsg` con `toolCalls`, incluidos los snapshots parciales. Si esa
tool call:

- no era de archivo (no crea `FilePanel`), o
- aún no tenía nombre resuelto,

el shimmer desaparecía y no había nada visible que lo reemplazara → el chat
parecía congelado.

### Solución

En el handler de `chatStreamMsg`, sólo apagamos `thinking` cuando:

- llega el snapshot **final** (`!v.partial`), o
- ya existe al menos un `FilePanel` visible (`len(m.panels()) > 0`).

Así el usuario siempre ve o el shimmer, o la ventana de archivo escribiendo,
o el texto del modelo — nunca un vacío.

## Archivos modificados

- `internal/providers/openai/codex_transport.go`
- `internal/tui/chat.go`

## Pruebas

- `go build ./...` OK.
- `go test ./...` OK (todos los paquetes verdes, incluidos `internal/tui`
  y `internal/tools`).

Manual (usuario, con suscripción Codex):

1. Login con `/login` (si venías de antes del fix 016, re-loguear para
   persistir `AccountID`).
2. Pedir una edición grande, por ejemplo: `Refactoriza X archivo` o
   `Reemplaza el bloque Y por Z en /ruta/archivo.go`.
3. Verificar que la ventana `Editando /ruta/archivo.go` aparece **antes**
   de que termine el turno, con el diff verde/rojo actualizándose en vivo.
4. Provocar una respuesta lenta (modelo con reasoning largo): comprobar
   que el shimmer "Pensando…" permanece visible mientras no llega nada.

## Commit

- Summary: Streaming en vivo de ediciones Codex y shimmer persistente
- Description: parseCodexSSE indexa las tool calls por output_index en
  lugar de call_id, lo que arregla el streaming de str_replace (los deltas
  de function_call_arguments viajan con item_id, no con call_id, por lo
  que antes se descartaban y el panel de edición sólo aparecía al final).
  Se añade el evento response.function_call_arguments.done y alias por
  item_id/call_id como fallback. En chat.go, el indicador "Pensando…" ya
  no se apaga con snapshots parciales de tool calls que aún no producen
  una ventana visible: sólo se oculta cuando hay un FilePanel abierto o
  llega el snapshot final.

## Riesgos

- Cambia la firma interna de `snapshotCodex`. No hay tests que la usen
  directamente, pero cualquier código nuevo debe pasar la nueva firma.
- El indicador puede quedar visible unos ms extra en tool calls no-archivo;
  es preferible a un chat en blanco.

## Próximos pasos

- Añadir un test de `parseCodexSSE` con fixtures reales de la Responses
  API (secuencia added → delta × N → done → completed) para blindar el
  contrato de eventos.
