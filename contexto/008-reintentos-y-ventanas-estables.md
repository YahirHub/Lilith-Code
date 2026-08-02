# 008 — Reintentos ante fallos del proveedor y ventanas de código estables

## Petición del usuario
1. Un `HTTP 500` del proveedor cortaba el turno y había que escribir «Continue»
   a mano: lo correcto es reintentar automáticamente.
2. La UIX saltaba mientras se transmitía una edición de código.
3. El panel de código no debe auto-plegarse: por defecto muestra una vista
   previa y `ctrl+o` la expande a todo el archivo (y vuelve a la vista previa),
   actuando siempre sobre la última ventana abierta.

## Implementación
- `internal/providers/openai/client.go`: `Stream` reintenta hasta 3 veces con
  espera creciente (1 s, 2 s). `isTransient` acepta 408/429/5xx y fallos de
  transporte (timeout, EOF, reset, DNS, TLS); rechaza el resto de HTTP (401,
  400…). El sink `countingSink` cuenta lo ya emitido: si el turno ya entregó
  contenido o tool calls, no se reintenta para no duplicar. `do` escribe por
  `out.send`, y los `select` con `ctx.Done()` usan `default` para no bloquear.
- `internal/tui/filepanel.go`: `Collapsed` se sustituye por `Expanded`
  (por defecto falso = vista previa). `Finish` ya no pliega nada. La vista
  previa tiene **alto fijo** (`previewLines = 12`, relleno con líneas en
  blanco): así el panel no cambia de altura entre fotogramas y el transcript
  deja de saltar durante el stream. Con más contenido se muestra
  «… N líneas más arriba (ctrl+o para ver todo)».
- `internal/tui/chat.go`: nuevo `panelPinned`. Mientras el usuario no navegue
  con `ctrl+j` / `ctrl+k`, `ctrl+o` actúa sobre la última ventana; al navegar
  el foco queda fijado. Tras alternar se hace `GotoBottom`. Las líneas de
  herramientas recuperan el separador `⚙ `.

## Reglas derivadas
- Cualquier vista en vivo dentro del transcript debe tener altura estable
  mientras el stream corre; si crece, la ventana desliza, no se estira.
- Regla original sustituida por `099-reconexion-automatica-y-skills-internas.md`: ante cortes de transporte se puede reintentar después del primer chunk únicamente si la TUI elimina antes todo el intento parcial y conserva intacto el request original. Los reintentos HTTP normales siguen evitando duplicados.

## Pruebas
`internal/tui/filepanel_test.go`: la vista previa mide siempre `previewLines`
y expandida muestra todo; `Finish` ya no pliega. `go build ./...` y
`go test ./...` en verde.
