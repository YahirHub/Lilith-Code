# 086 — Desacoplar streaming y render físico de Tview

## Problema observado

En conversaciones grandes, y ocasionalmente también en sesiones más pequeñas, la respuesta podía caer hasta aproximadamente un token por segundo. Los contadores `Elapsed` de comandos avanzaban tarde o saltaban varios segundos, lo que indicaba que el retraso no provenía necesariamente del proveedor: el propio runtime dejaba de procesar mensajes con regularidad.

## Causa

El loop lógico de `tviewRuntime` procesaba un mensaje, construía `RootModel.View()` y llamaba a `Application.QueueUpdateDraw` antes de ejecutar el comando que solicitaba el siguiente chunk del stream.

`QueueUpdateDraw` espera a que el event loop de Tview ejecute la actualización y complete el dibujo. Por tanto, una terminal lenta, un frame costoso o un transcript grande introducían backpressure directamente en:

- la lectura del siguiente chunk SSE;
- los ticks de `Elapsed`;
- los resultados de herramientas;
- el teclado y otros mensajes internos.

Además, aunque el Markdown del prefijo estable ya tenía caché, cada refresco concatenaba prefijo y cola y volvía a dividir todas las líneas del transcript. El coste seguía creciendo con el historial total.

## Corrección

### Render físico independiente

`tviewRuntime` separa ahora dos responsabilidades:

1. `modelLoop` consume mensajes, actualiza estado y despacha inmediatamente el siguiente `uikit.Cmd`.
2. `renderLoop` es el único que llama a `QueueUpdateDraw`.

Entre ambos existe una cola de frames `latest-only`: nunca bloquea al productor y conserva únicamente el frame más reciente. Los frames obsoletos se descartan porque no aportan información visual y no deben frenar el stream.

La generación de vistas se limita a 30 FPS. Esto mantiene una interfaz fluida sin intentar pintar un frame por cada token. Si una pantalla auxiliar está visible, los mensajes internos del chat continúan procesándose pero no fuerzan repintados de una pantalla que no cambió.

### Transcript segmentado

El viewport interno ya no exige un único string completo. Puede recibir varios segmentos de líneas:

- prefijo estable del historial, ya procesado y cacheado;
- cola mutable del turno en curso.

El viewport calcula scroll y filas visibles directamente sobre esos segmentos. Cada delta sólo vuelve a renderizar y dividir la cola mutable; no concatena ni divide cientos o miles de líneas históricas.

### Geometría del chat

`syncViewportGeometry` dejó de ejecutarse para cada delta SSE y cada tick. Se conserva antes de interacciones de teclado o ratón, que son los eventos que necesitan geometría exacta del frame visible.

## Invariantes

- Ninguna lectura de stream debe depender de que Windows Terminal/Linux termine de pintar.
- Sólo `renderLoop` puede llamar a `QueueUpdateDraw`.
- La cola visual debe ser no bloqueante y conservar el frame más reciente.
- El transcript estable no debe reconstruirse por token.
- Esc, cancelación, tool calls, timers y pantallas auxiliares deben seguir procesándose en orden.

## Pruebas añadidas

- La cola visual acepta 10,000 frames sin consumidor y no bloquea.
- Al consumirla entrega el último frame, no uno obsoleto.
- El `modelLoop` procesa 5,000 mensajes aunque no exista consumidor de frames, demostrando que el dibujo físico ya no introduce backpressure.
- El viewport segmentado conserva orden, conteo, scroll y compatibilidad con `SetContent`.

## Validación manual recomendada

1. Abrir una conversación extensa y solicitar una respuesta larga.
2. Confirmar que el texto fluye sin pausas proporcionales al historial.
3. Ejecutar un comando largo y observar que `Elapsed` avanza regularmente.
4. Abrir `/config` durante streaming y volver al chat; el turno debe continuar.
5. Desplazarse hacia arriba durante streaming y confirmar que no fuerza autoscroll ni degrada de forma marcada el terminal.
