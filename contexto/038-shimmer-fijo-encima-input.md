# 038 — Shimmer "Trabajando/Pensando" fijo encima del input

## Problema
El shimmer se pintaba dentro del transcript y desaparecía cuando:
- Se anexaban burbujas de tool (empujaban el shimmer fuera del viewport).
- Entre `toolResultsMsg` (working=false) y el siguiente `runTurn`
  (thinking=true) había un frame sin actividad marcada.

## Cambio
`internal/tui/chat.go`:
- Nuevo `pinnedActivityView(w)`: renderiza el shimmer como cromo FIJO justo
  encima del panel de cola / input. No se mueve con el scroll ni lo tapa
  ningún mensaje.
- `bottomChromeHeight` y `View()` reservan e insertan ese panel cuando hay
  actividad.
- `renderTranscript` ya NO pinta el shimmer (evita duplicados y saltos).
- `toolResultsMsg` deja de bajar `m.working` a false: el turno sigue vivo
  hasta que `runTurn` cambia a `thinking=true`, así no hay parpadeo.

## Descripción commit
`fix(tui): pin activity shimmer above input so it never scrolls or hides`
