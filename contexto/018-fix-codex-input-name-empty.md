# 017 · Fix HTTP 400 `input[i].name` vacío en Codex Responses

## Summary
Filtrar tool_calls sin nombre antes de enviarlos a la Responses API de Codex.

## Description
El backend `chatgpt.com/backend-api/codex/responses` rechaza con HTTP 400
(`empty_string`) cualquier item `function_call` cuyo campo `name` sea cadena
vacía. Cuando el SSE entregaba una tool_call incompleta (sin `name` en
`output_item.added/done`) o con `arguments` vacíos, la reenviábamos tal cual en
el siguiente turno, rompiendo toda la conversación.

Cambios en `internal/providers/openai/codex_transport.go`:

- `buildCodexPayload`: omite tool_calls sin `name`, normaliza `arguments`
  vacíos a `"{}"` y descarta `function_call_output` sin `call_id`.
- `snapshotCodex`: no expone al chat tool_calls con nombre vacío para evitar
  ejecutar "herramienta desconocida" fantasma.

Validado con `go build ./...`.
