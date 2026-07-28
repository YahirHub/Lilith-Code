# Tarea 09 — Pegado multilínea atómico en terminales degradadas

## Estado
pendiente

## Objetivo
Evitar que un texto pegado con saltos de línea se fragmente en múltiples solicitudes o mensajes de cola cuando el host/terminal no conserva el marcador de bracketed paste de Bubble Tea.

## Criterios de aceptación
- Un `KeyMsg` con `Paste=true` continúa insertándose atómicamente, sin envíos intermedios.
- Si el host degrada un paste a `texto -> Enter -> texto`, el primer Enter no dispara una solicitud ni la cola.
- Los párrafos posteriores permanecen dentro del mismo textarea.
- CRLF se trata como un único salto de línea, sin líneas vacías duplicadas.
- Mientras Lilith ya está trabajando, un paste multilínea no crea una entrada de cola por párrafo; sólo el Enter humano posterior puede encolar el bloque completo.
- Escribir normalmente y pulsar Enter muy rápido sigue enviando el mensaje; no vuelve la heurística antigua basada en la tecla anterior.
- Timers obsoletos no pueden enviar contenido después de que un Enter haya sido confirmado como parte de un paste.
- Shift/Alt/Ctrl+Enter continúan insertando una nueva línea explícita.
- No se agregan dependencias ni se migra Bubble Tea/Bubbles de versión en este fix.

## Restricciones
- Preservar los cambios previos no relacionados de `README.md` y `cmd/build/main.go`.
- Mantener una sola tarea `en-proceso`.
- Mantener compatibilidad con Bubble Tea v1.2.4 y Bubbles v0.20.0.

## Implementación actual
- El camino nativo sigue siendo `KeyMsg.Paste`, que es el mecanismo oficial de Bubble Tea v1.
- Para hosts que pierden los delimitadores bracketed-paste se aplica una máquina de estados: un Enter ambiguo se difiere 80 ms; contenido posterior inmediato lo confirma como salto de paste y, si no llega nada, se ejecuta el submit normal.
- Una vez confirmada la ráfaga, Enter permanece como salto hasta 60 ms de inactividad.
- Los pares CRLF se coalescen para insertar una sola nueva línea.
- Los timers usan secuencias monotónicas para descartar resoluciones antiguas.

## Validación pendiente
- Ejecutar `go test ./...` y `go vet ./...` en Windows con Go 1.24+.
- Repetir el pegado de la captura mientras hay un turno activo y confirmar que la cola no se fragmenta.
