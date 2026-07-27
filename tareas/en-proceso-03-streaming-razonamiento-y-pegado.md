# Tarea 03 — Streaming, razonamiento y pegado del chat

## Estado
En proceso.

## Objetivo
Corregir tres problemas del chat TUI:

1. No renderizar una cabecera vacía de `lilith` mientras la API todavía no ha emitido contenido.
2. Mostrar en vivo el razonamiento que los endpoints OpenAI-compatible expongan explícitamente (`reasoning_content`, `reasoning` o texto de `reasoning_details`), conservándolo cuando sea necesario en tool calls.
3. Eliminar la heurística temporal que convierte un Enter rápido en salto de línea y usar el soporte oficial de bracketed paste para pegar texto multilínea como un único bloque, sin simular escritura ni disparar cada salto como envío.

## Criterios de aceptación
- Tras enviar un mensaje se ve `Pensando…`, pero no `» lilith` vacío.
- `» lilith` aparece sólo al llegar el primer delta de respuesta textual o al materializar un cierre/fallback real.
- El razonamiento OpenAI-compatible visible por API aparece mientras se genera y el panel termina al comenzar texto/tool call o al cerrar el stream.
- Escribir una letra y pulsar Enter inmediatamente envía el mensaje.
- Pegar texto multilínea mediante bracketed paste conserva los saltos dentro del textarea y no crea solicitudes por cada línea.
- No se añade una dependencia nueva.

## Pruebas previstas
- Tests TUI para placeholder diferido, razonamiento visible, Enter inmediato y paste multilínea.
- Tests del cliente OpenAI-compatible para `reasoning_content` streaming.
- `gofmt`, `go test` y `go vet` en todo lo que permita el toolchain disponible.

## Implementado
- [x] Respuesta visual `lilith` creada sólo al primer delta textual o cierre real.
- [x] Parsing OpenAI-compatible de `reasoning_content`, `reasoning` y texto de `reasoning_details`.
- [x] Panel de razonamiento en vivo y conservación de `reasoning_content` en tool calls.
- [x] Eliminada la heurística temporal `lastKeyAt`/200 ms.
- [x] Enter simple siempre envía; Shift/Alt/Ctrl+Enter crea nueva línea.
- [x] Bracketed paste se inserta de una sola vez y conserva saltos internos.
- [x] Pruebas de regresión añadidas.

## Validación realizada
- `gofmt`: correcto.
- `git diff --check`: correcto.
- `go test ./internal/providers/openai ./internal/session`: correcto en copia temporal ajustada únicamente a Go 1.23, con `GOPROXY=off`.
- `go vet ./internal/providers/openai ./internal/session`: correcto bajo las mismas condiciones.
- La suite `internal/tui` queda pendiente porque el proyecto exige Go 1.24 y el sandbox no dispone de las dependencias Charm en caché ni puede acceder a `proxy.golang.org`.

## Validación local pendiente
```powershell
go test ./...
go vet ./...
go run .\cmd\li\
```

Probar además: Enter inmediato tras escribir, pegado multilínea grande, razonamiento streaming y una tool call con un modelo que exponga `reasoning_content`.
