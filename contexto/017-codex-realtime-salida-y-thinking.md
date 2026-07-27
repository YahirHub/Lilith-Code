---
name: Codex tiempo real, salida segura y panel de razonamiento
description: Fixes 017 — streaming en vivo de tool calls Codex, Ctrl+C con doble pulsación, Ctrl+Z ignorado, panel de reasoning summary plegable
type: feature
---
# 017 · Codex tiempo real, salida segura y panel de razonamiento

## Problemas
1. Con Codex (gpt-5.*), las herramientas de creación/edición sólo aparecían al final.
2. Ctrl+C cerraba el CLI de golpe; Ctrl+Z suspendía el proceso.
3. No había forma de ver el "pensamiento" (reasoning summary) del modelo.

## Cambios
- `internal/providers/openai/client.go` — `Chunk` gana `Thinking` y `ThinkingDone`.
- `internal/providers/openai/codex_transport.go`:
  - Se emite un snapshot parcial al recibir `response.output_item.added` con nombre, para que el panel de archivo se cree antes del primer delta de argumentos.
  - Se traducen `response.reasoning_summary_text.delta|done` y variantes a `Chunk.Thinking`.
- `internal/tui/thinking_panel.go` (nuevo) — `ThinkingPanel` estilo `FilePanel`: alto fijo (6 líneas), expandible/plegable.
- `internal/tui/chat.go`:
  - Nueva `MsgThinking` + campo `Thinking *ThinkingPanel` en `ChatMessage`.
  - `chatStreamMsg` propaga `thinking` y `thinkingDone`; el panel se inserta antes del mensaje del asistente en curso.
  - Toggle con **Ctrl+R**.
  - **Ctrl+C**: si hay tarea, cancela y "arma" salida por 2s; segundo Ctrl+C dentro de la ventana sale; si no hay tarea, el primer Ctrl+C sólo pide confirmación.
  - **Ctrl+Z**: se ignora (no cierra ni suspende).
- `internal/tui/commands.go` — help actualizada.

## Commit
- Summary: Streaming en vivo de Codex, salida segura y panel de razonamiento
- Description: Codex Responses ahora emite snapshots parciales al abrir cada `function_call`, por lo que las ventanas de creación/edición se pintan desde el primer delta en vez de aparecer al final. Los deltas de `reasoning_summary_text` se traducen a un `ThinkingPanel` plegable (Ctrl+R) con altura fija para que el transcript no salte. Ctrl+C cancela la tarea en curso y requiere una segunda pulsación en menos de 2s para salir; Ctrl+Z se ignora para evitar cierres accidentales. `/exit` sigue siendo la salida limpia.
