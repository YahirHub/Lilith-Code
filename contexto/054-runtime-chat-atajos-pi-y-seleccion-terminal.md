# 054 · Runtime persistente del chat, atajos tipo Pi y selección de terminal

Fecha: 2026-07-28

## Objetivo

Corregir el congelamiento que ocurría al abrir `/config` (u otra pantalla) durante una tarea activa y regresar con `Esc`, separar correctamente la cola de mensajes de la cancelación del turno y acercar los atajos del chat a Pi sin reemplazar los componentes existentes de configuración.

## Causa raíz del congelamiento

`RootModel` entregaba cada mensaje de Bubble Tea únicamente a la pantalla visible. El streaming del chat funciona como una cadena de comandos: cada `chatStreamMsg` procesado programa el siguiente `streamPump`. Si un chunk, un resultado de herramienta o un tick llegaba mientras `/config` estaba visible, esa pantalla lo descartaba y nunca se programaba el siguiente paso.

Al volver al chat, `streaming` seguía en `true`, pero ya no existía ningún pump activo. Por eso el input aceptaba texto y todo nuevo envío terminaba aparentemente atascado en la cola.

## Solución de runtime

- `RootModel` reconoce los mensajes pertenecientes al runtime persistente del chat.
- Mientras otra pantalla está visible, esos mensajes se entregan directamente a `ChatModel` sin cambiar la pantalla actual.
- El `tea.Cmd` devuelto por el chat se conserva, por lo que streaming, tools, timers y persistencia incremental siguen avanzando en segundo plano.
- `/config`, `/models`, `/providers` y el resto de componentes visuales no se sustituyen ni se reescriben.

## Cola y atajos alineados con Pi

Referencia funcional consultada en la documentación actual de Pi:

- `Enter`: steering. Si el agente trabaja, la instrucción se entrega en la siguiente frontera segura, después de las tools actuales.
- `Alt+Enter`: follow-up. Se ejecuta cuando el agente termina todo el trabajo actual.
- `Esc`: aborta el turno y devuelve los mensajes pendientes al editor.
- `Alt+Up`: devuelve la cola al editor sin cancelar el turno activo.
- `Ctrl+C`: limpia el editor; ya no cancela el turno ni descarta la cola.
- `Ctrl+D`: sale cuando el editor está vacío.
- `Ctrl+Z`: suspende en Unix; en Windows no hace nada porque no existe job control equivalente.

La cola ahora distingue explícitamente `steer` y `follow-up`. Un steering se consume de uno en uno en fronteras seguras; un follow-up sólo inicia un nuevo turno cuando el anterior ya terminó.

## Selección y copia de texto

Bubble Tea estaba iniciado permanentemente con mouse cell motion. Ese modo hace que el terminal entregue el drag del mouse a la TUI y evita la selección nativa cómoda del texto.

Ahora el modo de mouse se controla dinámicamente. El programa conserva `WithMouseCellMotion` al arrancar para que onboarding/settings sean interactivos desde el primer frame, pero `RootModel` cambia el modo según la pantalla:

- Chat: mouse reporting desactivado para que el terminal pueda seleccionar/copiar texto de forma nativa.
- Pantallas de settings/config/selectores: mouse cell motion activado para conservar clicks e interactividad.

Se mantiene `AltScreen`; no se reemplaza el renderer ni los componentes actuales.

## Archivos relevantes

- `internal/tui/app.go`
- `internal/tui/chat.go`
- `internal/tui/commands.go`
- `internal/tui/status_bar.go`
- `cmd/li/main.go`
- `internal/tui/app_background_test.go`
- `internal/tui/chat_pi_shortcuts_test.go`
- `internal/tui/chat_streaming_input_test.go`
- `internal/tui/chat_cancel_model_test.go`

## Pruebas añadidas

- Un chunk de chat recibido mientras otra pantalla está visible se procesa en `ChatModel`, no en la pantalla secundaria, y conserva el siguiente `streamPump`.
- `Ctrl+C` no cancela la tarea ni borra la cola.
- `Esc` cancela y restaura la cola en el editor.
- `Alt+Up` restaura la cola sin cancelar.
- `Alt+Enter` crea un follow-up.
- El steering se inyecta en la siguiente frontera de herramientas manteniendo el mismo turno.
- El paste multilínea durante streaming sigue entrando como un único mensaje de steering.

## Validación local pendiente

El entorno de trabajo disponible durante esta modificación sólo dispone de Go 1.23.2, mientras `go.mod` exige Go 1.24.0. `gofmt` y `git diff --check` sí pueden ejecutarse, pero `go test` debe correrse en un entorno con Go 1.24+.

Comandos recomendados:

```bash
go test ./internal/tui ./internal/tools
go test ./...
go build ./cmd/li
```

En Windows Terminal, `Alt+Enter` está reservado normalmente para fullscreen. Para recibir el mismo atajo que Pi, el terminal debe reenviar la combinación al proceso (Pi documenta el escape `\\u001b[13;3u`).
