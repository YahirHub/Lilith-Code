# 033 – Heurística anti-paste y errores del proveedor

## Contexto
En Windows PowerShell clásico el modo *bracketed paste* no siempre llega a
Bubbletea (`v.Paste` = false), por lo que un pegado multi-línea se convertía
en varios Enter y lanzaba una tarea nueva por cada salto de línea, en vez de
quedar como texto literal en el textarea.

Además, cuando el gateway devolvía un error plano
`{"message":"Error from provider (Console): ...","code":"invalid_request_error"}`
el cliente OpenAI-compatible no reconocía ese envelope y el usuario veía
`invalid character 'p' looking for beginning of value` como error real.

## Decisión
1. **Heurística de paste** en `internal/tui/chat.go`:
   - Se guarda `lastKeyAt` en cada `KeyMsg` (excepto Enter).
   - Si un Enter llega a < 25 ms de la tecla previa, se inserta `\n` en el
     textarea en lugar de enviar. Así el pegado queda como texto literal aun
     sin bracketed paste real.
   - `shift+enter`, `alt+enter` y `ctrl+enter` insertan salto de línea de
     forma explícita para composición manual multilínea.
   - Se preserva el path existente de `v.Paste` (bracketed paste real) y toda
     la lógica de Ctrl+C / cola de tareas.

2. **Errores del proveedor** en `internal/providers/openai/client.go`:
   - `chatResponse` reconoce ahora el envelope plano
     `{"message":"...","code":"..."}` que usan algunos gateways.
   - Se reporta `raw.Message` como error tanto en modo streaming como en
     modo no-streaming, evitando el ruido de `invalid character 'p'`.

## Verificación
- `go build ./...` OK.
- `go test ./...` OK.

## Commit sugerido
**Título:** `fix(tui): heurística anti-paste y mejor reporte de errores del gateway`

**Descripción:**
- Añade heurística de tiempo entre teclas para tratar Enter dentro de un
  pegado multi-línea como salto de línea en terminales sin bracketed paste.
- Añade shift/alt/ctrl+Enter como salto de línea explícito.
- Reconoce el envelope de error `{message, code}` que devuelven gateways
  tipo Console/OpenRouter y lo muestra en vez del error de parseo JSON.
