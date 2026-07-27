# 004 — Corrección: las tool calls se perdían en el stream

## Síntoma

Al pedir "Crea un html base de ejemplo", Lilith no respondía nada: aparecía la
burbuja del asistente vacía y el turno terminaba en silencio.

## Causa

`streamPump` (internal/tui/chat.go) reenviaba a la TUI únicamente `c.Delta` y
descartaba `c.ToolCalls`. El cliente OpenAI sí acumulaba correctamente las
llamadas fragmentadas y emitía un `Chunk{ToolCalls: ...}` al cerrar el stream,
pero ese chunk llegaba al `Update` con `delta` vacío y sin llamadas, así que:

- `m.pendingCall` quedaba vacío,
- el turno terminaba sin texto y sin ejecutar herramientas,
- la pantalla mostraba una respuesta vacía.

Es el caso más frecuente cuando el modelo responde sólo con tool calls (que es
justo lo que hace al pedirle crear un archivo).

## Solución

1. `streamPump` propaga `toolCalls: c.ToolCalls` en el `chatStreamMsg`.
2. Si un turno termina sin texto y sin herramientas, se muestra
   `(el modelo no devolvió contenido)` en vez de una burbuja vacía, para que el
   fallo sea visible en lugar de parecer un cuelgue.

## Regla derivada

Cualquier campo nuevo de `openai.Chunk` debe propagarse explícitamente en
`streamPump`; es el único puente entre el cliente y el bucle del agente.