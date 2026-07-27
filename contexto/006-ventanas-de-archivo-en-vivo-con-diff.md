# 006 — Ventanas de archivo en vivo con diff

## Petición del usuario
Al crear o editar un archivo, la TUI debe mostrar el trabajo en tiempo real
dentro de una ventana plegable/expandible, con el código nuevo en verde y, en
las ediciones, lo eliminado en rojo (estilo GitHub). Esa ventana reemplaza las
líneas crudas `write_file path: "..." content: "..."` del chat.

## Implementación
- `internal/providers/openai/client.go`: `Chunk` gana el campo `Partial`. El
  lector SSE emite una instantánea de las tool calls cada vez que crecen sus
  argumentos, además del envío final. Regla: todo campo nuevo de `Chunk` debe
  propagarse por `streamPump` (ver 004).
- `internal/tui/filepanel.go` (nuevo): tipo `FilePanel` con render de ventana
  redondeada, cabecera `▾/▸ Creando ruta +N -M`, cuerpo en verde para
  `write_file` y diff LCS verde/rojo para `str_replace`, más
  `partialJSONString` que extrae `path`/`content`/`old`/`new` de un JSON aún
  incompleto (incluye escapes `\n`, `\t`, `\uXXXX` truncados).
- `internal/tui/chat.go`: nuevo `MsgFile` con puntero a panel. `applyToolCalls`
  crea/refresca paneles por índice de tool call y los indexa por `ToolCallID`
  para cerrarlos con el resultado real (`Finish`). Las herramientas de archivo
  ya no generan líneas `⚙ write_file …` ni `↳ …`; el resto de herramientas
  conserva su formato compacto.
- Mientras se escribe sólo se pintan las últimas 18 líneas; al terminar, un
  archivo largo se pliega solo.
- Teclas: `ctrl+o` expande/contrae el panel (ver 008), `ctrl+j` / `ctrl+k`
  cambian de panel.
- La burbuja del asistente se localiza con `lastAssistantIndex()` porque los
  paneles se insertan después de ella durante el stream.

## Reglas derivadas
- Cualquier herramienta mutante de archivos nueva debe declararse en
  `IsFileTool` y renderizarse como panel, nunca como texto crudo en el chat.
- Los argumentos parciales nunca se pasan a `json.Unmarshal`: sólo el lector
  tolerante `partialJSONString`.

## Pruebas
`internal/tui/filepanel_test.go`: JSON parcial, contenido en vivo, diff
verde/rojo y plegado automático. `go build ./...` y `go test ./...` en verde.
