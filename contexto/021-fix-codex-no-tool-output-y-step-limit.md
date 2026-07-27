# 020 — Codex: "No tool output found for function call ..." + límite de pasos bajo

## Síntomas (captura del usuario)
1. `X límite de pasos de herramientas alcanzado en este turno` a los 25 pasos, cortando ediciones legítimas.
2. Al mensaje siguiente:
   ```
   X Error del proveedor: HTTP 400: {"error":{"message":"No tool output found for
     function call call_9NgnCASPzt9NCsz06pcFE5Xt","type":"invalid_request_error",
     "param":"input","code":null}}
   ```

## Causa
La Responses API de Codex exige que cada `function_call` en `input` esté seguido
(o acompañado) de su `function_call_output` con el mismo `call_id`. El historial
quedaba con **assistant.ToolCalls sin sus mensajes `tool` pareja** en tres rutas:

- **Límite de pasos**: `runTools` devolvía error tras appendear el assistant con
  tool_calls al historial, pero nunca ejecutaba las herramientas → no había
  outputs.
- **Ctrl+C durante ejecución de herramientas**: mismo problema; `pendingCall`
  quedaba con calls fantasma en historial sin outputs.
- **Reintento server-side (superseded)**: aunque el transporte ya saca la call
  huérfana de `order` (fix 019), cualquier ruta futura que se olvide de sanear
  también rompía la sesión entera.

## Fix
### `internal/providers/openai/codex_transport.go` — defensa en profundidad
Al final de `buildCodexPayload`, escanea `input` y para cada `function_call` sin
`function_call_output` pareja inyecta un stub:
```json
{ "type":"function_call_output", "call_id": "...",
  "output": "error: la ejecución de esta herramienta no se completó (turno interrumpido)." }
```
Esto blinda el request contra cualquier ruta que deje calls huérfanas.

### `internal/tui/chat.go` — saneo en origen + tope subido
- `maxToolSteps`: **25 → 60**. Ediciones grandes encadenan `read_file` +
  `str_replace` y pasan 25 con facilidad; 60 sigue evitando bucles infinitos.
- Al hit del step-limit, inyecta `tool` sintéticos ("turno interrumpido…") en
  `m.history` para todas las calls del último assistant.
- En `Ctrl+C` durante streaming: si hay `pendingCall`, inyecta `tool` sintéticos
  ("cancelado por el usuario") antes de vaciar `pendingCall`.
- Nuevo import: `fmt` (para el número dinámico en el mensaje).

## Verificación
- `go build ./...` ok.
- `go test ./...` ok.

## Commit sugerido
- **subject**: `fix(codex): evitar HTTP 400 por function_call sin output y subir tope de pasos a 60`
- **description**: La Responses API rechaza el request si un `function_call` en `input` no tiene su `function_call_output`. Ocurría cuando el turno se cortaba a la mitad — límite de pasos, Ctrl+C, o reintento del backend — dejando el historial con assistant.ToolCalls sin las respuestas `tool`. Ahora `buildCodexPayload` inyecta un stub de output para cada function_call huérfano (defensa en profundidad), y la TUI saneia el historial en origen: al hit del step-limit y en Ctrl+C durante streaming añade mensajes `tool` sintéticos con explicación humana. Sube además `maxToolSteps` 25→60 porque ediciones legítimas lo alcanzaban.
