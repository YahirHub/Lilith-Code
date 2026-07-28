# Fecha
2026-07-28

# Objetivo
Corregir la fragmentación de pegados multilínea cuando el terminal o una capa intermedia no entrega los delimitadores de bracketed paste y Bubble Tea recibe los saltos pegados como eventos Enter normales.

# Decisiones tomadas
- Mantener `KeyMsg.Paste` como camino principal y autoritativo; Bubble Tea v1.2.4 habilita bracketed paste por defecto mientras no se use `WithoutBracketedPaste`.
- No migrar Bubble Tea/Bubbles a v2 dentro de este bugfix. v2 ofrece mensajes de paste dedicados, pero supone una migración mayor del stack Charm y no soluciona por sí sola un host que elimine los delimitadores antes de llegar al proceso.
- No recuperar la heurística eliminada en ADR 042 que miraba cuánto tiempo había pasado entre una tecla y Enter. Esa estrategia confundía a usuarios que escriben y envían rápido.
- Para el caso degradado, diferir únicamente un Enter ambiguo durante 80 ms. Si llega contenido inmediatamente DESPUÉS, el Enter se convierte en salto de línea; si queda solo, se procesa como submit normal.
- Una vez confirmada una ráfaga de paste, mantenerla 60 ms de inactividad para preservar los saltos posteriores y posibles líneas finales.
- Coalescer CRLF: un CR seguido por LF representa una sola nueva línea.
- Usar IDs secuenciales en timers para que un timer antiguo jamás pueda enviar contenido que ya fue clasificado como paste.

# Arquitectura actual
- `internal/tui/chat.go` sigue manejando el textarea desde `ChatModel.Update`.
- `KeyMsg.Paste=true` inserta el bloque completo mediante `textarea.InsertString`, normalizando CRLF/CR a LF.
- El fallback mantiene dos estados independientes: `pendingEnter` para la decisión de un Enter aislado y `pasteFallbackActive` para una ráfaga ya confirmada.
- `pasteEnterDecisionMsg` ejecuta el submit sólo si su secuencia sigue vigente.
- `pasteFallbackIdleMsg` termina la ráfaga confirmada sin tocar el contenido.
- La cola continúa existiendo para mensajes escritos realmente durante un turno activo; el fallback evita que los párrafos de un mismo paste lleguen a `submit` por separado.

# Librerías usadas
No se agregaron dependencias. Se mantienen `github.com/charmbracelet/bubbletea v1.2.4` y `github.com/charmbracelet/bubbles v0.20.0`.

# Archivos importantes modificados
- `internal/tui/chat.go`
- `internal/tui/chat_streaming_input_test.go`
- `contexto/042-streaming-razonamiento-y-pegado.md`
- `contexto/050-pegado-multilinea-atomico.md`
- `tareas/pendiente-08-componentes-ajustes-providers-login.md`
- `tareas/en-proceso-09-pegado-multilinea-atomico.md`

# Problemas encontrados
- La regresión anterior sólo cubría el caso ideal `KeyMsg.Paste=true`. No simulaba el flujo visto en Windows donde un paste se degrada a eventos ordinarios.
- En ese flujo, el primer `KeyEnter` llamaba inmediatamente a `submit`; como el modelo empezaba a trabajar, los párrafos restantes llegaban después a `submit` y se almacenaban en `queue`, reproduciendo exactamente la captura del usuario.
- Sin delimitadores bracketed-paste, un CR pegado y una tecla Enter humana son indistinguibles en el instante exacto en que llegan. Cualquier solución que decida usando sólo el evento actual necesariamente adivina.
- Los pares CRLF requieren tratamiento explícito en el fallback para evitar dobles saltos.

# Soluciones implementadas
- Prueba de regresión para paste bracketed y para paste degradado `texto/CRLF/texto`.
- Enter ambiguo diferido con una ventana muy corta y decisión por evidencia posterior, no por velocidad previa de escritura.
- Estado de ráfaga temporal para impedir submits intermedios y fragmentación de la cola.
- Coalescencia CRLF y protección contra timers obsoletos.
- Prueba específica con `streaming=true` para verificar que un paste de tres párrafos produce cero entradas parciales de cola y, al enviarse manualmente, una sola entrada completa.

# Pendientes
- Ejecutar la suite completa con Go 1.24+ y las dependencias Charm reales.
- Validar manualmente el comportamiento en Windows Terminal/cmd.exe y PowerShell con el mismo contenido de la captura.
- Considerar una migración separada a Bubble Tea/Bubbles v2 únicamente como tarea de arquitectura, no como parche de este bug.

# Próximos pasos
1. Ejecutar `go test ./...` y `go vet ./...` en Windows.
2. Pegar un texto grande con párrafos y líneas vacías mientras Lilith está pensando y verificar que el panel de cola no aparece por cada párrafo.
3. Escribir una frase y pulsar Enter inmediatamente para confirmar que se envía tras la ventana imperceptible sin insertar un salto.
