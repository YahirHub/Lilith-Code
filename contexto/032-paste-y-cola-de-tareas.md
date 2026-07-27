# 032 · Pegado del portapapeles + cola de tareas + Ctrl+C

## Problema
Al pegar texto multilínea en el input, cada `\n` se disparaba como Enter y
lanzaba una tarea nueva. Además, mandar un mensaje mientras Lilith ya estaba
trabajando arrancaba un segundo turno en paralelo.

## Cambios (`internal/tui/chat.go`)
1. **Paste guard**: en el handler de `tea.KeyMsg`, si `v.Paste` es `true`
   (bracketed paste), el evento va siempre al textarea como texto literal.
   Nunca se interpreta como Enter, comando ni atajo aunque contenga saltos
   de línea o control chars.
2. **Cola de una tarea a la vez**: nuevo campo `queue []string` en
   `ChatModel`. Si `m.streaming` es `true` cuando el usuario envía, el
   mensaje se encola y se muestra `📥 En cola (posición N): …` en vez de
   arrancar un turno paralelo.
3. **Drenaje automático**: al terminar el turno actual (rama `v.done` de
   `chatStreamMsg`) se llama a `drainQueue()`, que hace pop del primer
   mensaje y lo re-somete vía `submit(next)`.
4. **Ctrl+C ampliado (sin romper lo anterior)**:
   - Con tarea activa: cancela como antes + limpia la cola pendiente
     (avisa cuántos mensajes se descartaron).
   - Sin tarea activa pero con cola: primer Ctrl+C vacía la cola.
   - Sin tarea ni cola: mantiene el doble Ctrl+C para salir.
5. Placeholder actualizado con la nueva semántica.

## Ctrl+C revisado (para no romper flujos)
- `onboarding.go`, `login_custom.go`, `login_codex.go`, `config_screen.go`,
  `model_selector.go`, `history_screen.go`: sin cambios (cada pantalla ya
  maneja Ctrl+C localmente para salir/cancelar).
- Chat: se conservan las 3 modalidades (cancelar tarea, doble-tap salir);
  se añade la de vaciar cola.

## Tests
`go build ./...` ✓ · `go test ./...` ✓ (todos los paquetes previos verdes,
sin regresiones).

## Commit propuesto
- **Summary**: Soporte de pegado y cola de una-tarea-a-la-vez en el chat
- **Description**: Bracketed paste ahora va siempre al textarea (fin del
  split línea-por-línea). Mientras hay una tarea en curso, los nuevos envíos
  se encolan y se drenan al terminar; Ctrl+C cancela la activa y limpia la
  cola (o vacía la cola si no hay tarea) sin romper el doble-tap para salir.
