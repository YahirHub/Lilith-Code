# Fecha
2026-07-27

# Objetivo
Corregir tres defectos del chat TUI: evitar la cabecera vacía de `lilith` antes de que el proveedor responda, mostrar el razonamiento que un endpoint OpenAI-compatible exponga durante el streaming y eliminar la heurística temporal de Enter que hacía que una escritura rápida insertara un salto de línea en vez de enviar.

# Decisiones tomadas
- La respuesta `MsgAssistant` se materializa de forma perezosa: no existe hasta que llega el primer delta textual o hasta que el stream termina y existe un cierre/fallback real.
- El indicador fijo `Pensando…` sigue visible durante la latencia previa al primer delta, pero ya no crea una respuesta vacía en el transcript.
- El cliente OpenAI-compatible reconoce razonamiento explícito en `reasoning_content`, `reasoning` y texto/resumen de `reasoning_details`.
- El razonamiento se representa con el `ThinkingPanel` existente y permanece expandido mientras se transmite; se cierra cuando comienza contenido textual, una tool call o termina el stream.
- `reasoning_content` se conserva en el historial interno. Es especialmente necesario para continuaciones de tool calls de proveedores que exigen reenviarlo.
- Se elimina por completo `lastKeyAt` y el umbral temporal de 200 ms introducido en ADR 034. Enter normal siempre envía; Shift/Alt/Ctrl+Enter insertan una nueva línea.
- Los pegados multilínea se distinguen mediante `KeyMsg.Paste` de Bubble Tea y se insertan de una sola vez con `textarea.InsertString`, normalizando CRLF/CR a LF. No se simula escritura carácter por carácter.
- ADR 033 y ADR 034 se conservan como historial de decisiones, pero la heurística temporal descrita allí queda reemplazada por este ADR.

# Arquitectura actual
- `internal/providers/openai` sigue siendo el adaptador mínimo para Chat Completions OpenAI-compatible y traduce los campos de reasoning a `Chunk.Thinking`.
- `internal/tui/chat.go` mantiene dos estados separados durante un turno: `reasoningBuf` para razonamiento expuesto por la API y `streamBuf` para la respuesta textual.
- `assistantActive` apunta sólo a la respuesta textual activa del subturno y evita editar accidentalmente una respuesta anterior cuando todavía no existe una burbuja del asistente.
- El historial completo continúa almacenándose en `[]openai.Message`; el razonamiento también participa en la estimación aproximada de contexto.

# Librerías usadas
No se agregaron dependencias. Se reutilizan Bubble Tea/Bubbles ya presentes y la librería estándar de Go.

# Archivos importantes modificados
- `internal/providers/openai/client.go`
- `internal/providers/openai/client_reasoning_test.go`
- `internal/tui/chat.go`
- `internal/tui/chat_streaming_input_test.go`
- `internal/tui/context_bar.go`
- `tareas/en-proceso-03-streaming-razonamiento-y-pegado.md`

# Problemas encontrados
- `runTurn()` agregaba inmediatamente un `MsgAssistant` vacío, por lo que la cabecera `» lilith` aparecía aun cuando la API no había emitido ningún byte útil.
- El cliente genérico sólo consumía `content` y tool calls; el razonamiento que gateways/modelos entregaban mediante campos OpenAI-compatible se descartaba.
- La heurística anti-paste de 200 ms convertía un Enter humano rápido en salto de línea. Era imposible distinguir de forma fiable paste y escritura real usando únicamente latencia entre teclas.
- Procesar pegados como muchos eventos individuales podía producir sensación de escritura progresiva y, en terminales problemáticos, interpretar saltos del texto como envíos.

# Soluciones implementadas
- Creación diferida de la respuesta visual de Lilith.
- Parsing y streaming de reasoning explícito con panel en vivo.
- Conservación de `reasoning_content` junto a mensajes assistant y tool calls.
- Eliminación de la heurística temporal y uso directo del indicador `Paste` de Bubble Tea.
- Inserción atómica de pegados y atajos explícitos para nueva línea.
- Pruebas de regresión para placeholder diferido, Enter inmediato, paste multilínea, panel de razonamiento y preservación de reasoning en tool calls.

# Pendientes
- Ejecutar `go test ./...` y `go vet ./...` con Go 1.24 y las dependencias Charm disponibles; el sandbox actual sólo dispone de Go 1.23.2 y no puede descargar el toolchain/dependencias.
- Validar manualmente el pegado en Windows Terminal/PowerShell y, si se requiere compatibilidad con una terminal que realmente no implemente bracketed paste, resolverla con una estrategia explícita para esa terminal en vez de reintroducir una heurística temporal global.
- Verificar con cada gateway concreto qué campo de reasoning expone; si no transmite ninguno, la TUI sólo puede mostrar `Pensando…` porque no existe razonamiento observable en la respuesta.

# Próximos pasos
1. Compilar y ejecutar las pruebas en Windows con Go 1.24+.
2. Probar escritura rápida seguida de Enter y un pegado multilínea grande.
3. Probar un modelo de razonamiento y una tool call para confirmar que el panel se actualiza y la continuación no devuelve errores 400.
