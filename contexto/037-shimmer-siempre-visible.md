# 037 — Shimmer "Trabajando/Pensando" siempre visible

## Problema
El shimmer se renderizaba dentro del último mensaje del asistente sólo cuando
éste era la última entrada del transcript. Al ejecutar herramientas los
mensajes de tool quedan al final, así que "Trabajando…" nunca aparecía.

## Cambio
En `internal/tui/chat.go` (`renderTranscript`):
- Se removió la rama que pintaba el shimmer dentro del bloque `MsgAssistant`.
- Se agrega un bloque final tras el loop: si `m.thinking || m.working`, se
  anexa `RenderWorking` (verde) o `RenderThinking` (púrpura) al pie del
  transcript, independientemente del tipo de mensaje anterior.

## Descripción commit
`fix(tui): render shimmer trabajando/pensando siempre al pie del transcript`
