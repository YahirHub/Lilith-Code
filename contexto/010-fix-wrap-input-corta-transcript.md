# Fecha
2026-07-27

# Objetivo
Corregir bug visual del TUI: al escribir un mensaje largo, la parte final de la
última respuesta del CLI se recortaba/tapaba (por ejemplo, la salida de `$ ls`
desaparecía debajo de la caja de entrada).

# Decisiones tomadas
- Calcular el alto del textarea usando el número de **líneas visuales**
  (`textarea.LineCount()`), no sólo el conteo de `\n`.
- Mantener el conteo lógico (`strings.Count(val, "\n")+1`) como piso, por si
  `LineCount()` reporta 0 antes del primer render.
- Conservar el tope de 8 líneas (coincide con `ta.MaxHeight = 8`).

# Arquitectura actual
Sin cambios estructurales. Sólo cambia la lógica interna de
`internal/tui/chat.go::syncInputHeight`, que se invoca desde `updatePalette` y
tras cada cambio del textarea. `Resize` sigue derivando `vpHeight` de
`m.textarea.Height() + 2`; ahora ese valor ya refleja el wrap real, así que el
viewport se encoge correctamente y `GotoBottom` mantiene visibles las últimas
líneas del transcript.

# Librerías usadas
Sin cambios. `github.com/charmbracelet/bubbles/textarea` v0.20.0 ya expone
`LineCount()`.

# Archivos importantes modificados
- `internal/tui/chat.go` — `syncInputHeight` reescrita para usar `LineCount()`.

# Problemas encontrados
- El textarea envuelve texto por ancho, pero `syncInputHeight` sólo contaba
  saltos de línea explícitos, así que su alto reservado quedaba en 1 mientras
  la caja se dibujaba con 2 filas visuales. La segunda fila pisaba el final
  del transcript en cada re-render tras cada tecla.
- Reproducción: ventana angosta + mensaje largo en una sola línea → al pasar
  el ancho del textarea el prompt se "come" la última línea de la respuesta
  previa.

# Soluciones implementadas
`lines := max(strings.Count(val,"\n")+1, m.textarea.LineCount())`, clamp
`[1..8]`, luego `SetHeight` y `Resize` como antes.

# Pendientes
Ninguno directo. Vigilar que al cambiar `MaxHeight` (hoy 8) se ajuste también
el clamp para mantener consistencia.

# Próximos pasos
- Probar en Windows PowerShell (el reporte original venía de ahí) con ventanas
  angostas y mensajes multilínea.
- Si aparece otro caso donde el transcript se recorta, revisar también
  `Resize` (línea ~180 de `chat.go`) por si el margen `-1` final necesita
  ajustarse según terminal.
