# Fecha
2026-07-28

# Objetivo
Evitar que una cancelación rápida con Ctrl+C, un cierre inmediato de la CLI o una interrupción del proceso hagan que `/history`/`--continue` recupere únicamente el mensaje del usuario y pierda el razonamiento, respuesta parcial y progreso de herramientas que ya era visible en pantalla.

# Decisiones tomadas
- Separar el historial API (`Session.Messages`) del transcript visual (`Session.Transcript`).
- Mantener `Messages` siempre protocol-correcto para proveedores compatibles; una tool call parcial nunca se inserta artificialmente en ese historial.
- Persistir el mensaje del usuario inmediatamente como snapshot estable, como ya hacía Lilith.
- Durante streaming guardar un sidecar `<session>.live` sólo con la cola mutable del turno actual, no con toda la conversación.
- Agrupar checkpoints de tokens a un máximo de 5 escrituras por segundo (200 ms) y hacer el I/O fuera del goroutine `Update` de Bubble Tea.
- En fronteras críticas (tool call completa, resultado de herramienta, preflight y Ctrl+C) forzar un checkpoint pequeño antes de continuar.
- Ctrl+C no reserializa todo el historial: guarda sincrónicamente únicamente el sidecar del turno actual para mantener la cancelación inmediata.
- Cada snapshot/checkpoint tiene una revisión monotónica. Un sidecar atrasado nunca puede sobreescribir lógicamente un snapshot estable más nuevo.
- Al reanudar una sesión con checkpoint live, el transcript parcial se recupera, los paneles que quedaron ejecutándose se marcan como cancelados/interrumpidos y las tool calls completas sin output reciben un resultado sintético de cancelación para mantener el protocolo válido.

# Arquitectura actual
Persistencia estable:
`<config>/projects/<proyecto>/chats/<id>.json`
- metadata de sesión
- `Messages`: historial enviado al modelo
- `Transcript`: representación visual completa y restaurable
- `Revision`: versión del snapshot

Persistencia mutable:
`<config>/projects/<proyecto>/chats/<id>.live`
- revisión
- índice base del transcript estable
- índice base del historial API estable
- entradas visuales del turno actual
- cola de mensajes API seguros generados desde el último snapshot estable

Al finalizar normalmente un turno, el sidecar se promueve implícitamente al snapshot completo y se elimina.

# Librerías usadas
Sólo librería estándar de Go (`encoding/json`, `os`, `sync`, `time`) y las dependencias TUI ya existentes. No se agregaron dependencias.

# Archivos importantes modificados
- `internal/session/session.go`
- `internal/session/session_test.go`
- `internal/tui/chat.go`
- `internal/tui/chat_cancel_model_test.go`
- `internal/tui/filepanel.go`
- `tareas/en-proceso-10-persistencia-progreso-tiempo-real.md`

# Problemas encontrados
1. `persist()` sólo copiaba `m.history`; el reasoning y respuesta aún en streaming vivían únicamente en `m.messages`/buffers.
2. Las tool calls y resultados intermedios sólo se guardaban al terminar todo el turno.
3. `cancelTurn()` no persistía después de crear los outputs sintéticos de herramientas ni después del aviso de cancelación.
4. Guardar el JSON completo por cada token solucionaría pérdida de datos, pero volvería a introducir lag conforme creciera el historial.
5. Una escritura asíncrona vieja del sidecar podía terminar después de un guardado nuevo; se necesitaba control de revisiones.

# Soluciones implementadas
- Transcript visual serializable independiente del historial API.
- Checkpoint live incremental y atómico.
- Revisión monotónica y guard contra sidecars obsoletos.
- Primer delta/reasoning dispara un checkpoint inmediatamente; los siguientes se agrupan a 200 ms.
- Tool calls completas y outputs se checkpointan antes de iniciar la siguiente fase.
- Ctrl+C guarda el progreso mutable antes de devolver el control al usuario.
- Recuperación de assistant parcial seguro y reparación de tool outputs faltantes al reanudar.
- `FilePanel` incorpora estado `Canceled` para no mostrar eternamente `running` tras una interrupción.

# Pendientes
- Validar `go test ./...` y `go vet ./...` con Go 1.24+ y las dependencias Charm reales en Windows.
- Validar manualmente una conversación larga: cancelar durante reasoning, durante streaming textual y durante un comando largo; después reanudar con `/history` o `--continue`.

# Próximos pasos
Una vez validado en Windows, marcar la tarea 10 como completada y considerar una futura estrategia de limpieza/retención de sesiones si el historial llega a ocupar mucho espacio en disco.
