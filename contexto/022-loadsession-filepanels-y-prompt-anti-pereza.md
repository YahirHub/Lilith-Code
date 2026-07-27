# 021 · Rehidratar FilePanels al reanudar y prompt anti-pereza (pi-inspired)

## Problema

1. Al reabrir un chat guardado, las tool calls de `write_file` / `str_replace`
   se dibujaban como una línea de texto genérica (`⚙ write_file {"path":...}`)
   en lugar de conservar el diseño del panel con diff, título "Creado/Editado"
   y contador `+N -M`. Rompía la continuidad visual entre el turno en vivo y
   el turno reanudado.
2. El modelo estaba siendo perezoso: pedir "una página HTML en inglés y otra
   en español" producía sólo el esqueleto de la primera con un comentario
   `<!-- aquí va el resto -->` en lugar de dos archivos completos. El prompt
   anterior no prohibía explícitamente los placeholders ni exigía entregar
   todos los archivos pedidos en el mismo turno.

## Cambios

- `internal/tui/chat.go` → `LoadSession`: reconstruye `FilePanel` para cada
  `ToolCall` cuyo nombre pase `IsFileTool`, aplica `panel.Update(arguments)`
  con los args ya completos que están en el historial y empareja el
  resultado (`role: "tool"`) por `ToolCallID` llamando a `panel.Finish()`.
  Los paneles sin resultado (turno cortado) se marcan `Superseded` para no
  dejar el shimmer "escribiendo…" eterno.
- `internal/tui/chat.go` → `systemPrompt`: reescrito con estructura tipo pi
  (identidad + inventario de herramientas + reglas numeradas). Reglas duras
  nuevas: prohibición explícita de placeholders (`// resto`, `...`,
  `<!-- TODO -->`), obligación de entregar TODOS los archivos pedidos en
  el mismo turno (caso "misma página en dos idiomas"), y de seguir
  trabajando hasta terminar en vez de preguntar "¿continúo?".

## Verificación

- `go build ./...` ok.
- `go test ./...` ok.

## Commit sugerido

```
tui: rehidratar FilePanels al reanudar chat y endurecer prompt

- LoadSession reconstruye write_file/str_replace como FilePanel con
  diff y estado Finish/Superseded (antes: línea de texto genérica).
- systemPrompt inspirado en pi: prohíbe placeholders, exige entregar
  todos los archivos pedidos en el mismo turno y trabajar hasta
  completar la tarea.
```

**description**: rehidrata paneles de archivo al reanudar y endurece el
prompt para eliminar entregas perezosas.

**summary**: la sesión reanudada vuelve a mostrar los diffs con el mismo
diseño que en vivo, y el modelo ya no puede cerrar un turno con
placeholders ni omitir el segundo archivo de una tarea multi-archivo.
