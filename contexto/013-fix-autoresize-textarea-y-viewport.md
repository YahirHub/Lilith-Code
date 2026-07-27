# 013 - Fix autoresize textarea y viewport del transcript

## Fecha
2026-07-27

## Problema
El bug persistía: al escribir texto largo en el input de la TUI desaparecía o se corrompía visualmente parte de la última respuesta/mensaje del CLI.

## Investigación
Se contrastó el comportamiento con la implementación oficial de `github.com/charmbracelet/bubbles/textarea` v0.20.0 y `viewport`:

- `textarea.View()` reconstruye contenido del viewport interno durante renderizado.
- En Bubbles v0.20.0, cuando una app anfitriona auto-redimensiona el textarea según líneas soft-wrapped, el viewport interno puede conservar un `YOffset` obsoleto.
- Ese estado obsoleto hace que se oculten filas envueltas aunque el textarea ya tenga alto suficiente.
- `viewport.GotoBottom()` también depende de que el contenido haya sido seteado ya con el ancho final/envuelto; si se setea contenido sin envolver y luego cambia el layout, el fondo visible puede quedar mal calculado.

## Cambio aplicado
Archivo principal: `internal/tui/chat.go`.

1. Se agregó `refreshTranscript(scrollBottom bool)` para centralizar el render del transcript:
   - Renderiza el transcript.
   - Lo envuelve explícitamente al ancho actual del viewport con Lipgloss antes de `SetContent`.
   - Solo luego llama a `GotoBottom()`.

2. Se reemplazaron llamadas directas a:
   - `m.viewport.SetContent(m.renderTranscript())`
   - `m.viewport.GotoBottom()`

   por `m.refreshTranscript(true/false)`.

3. Se ajustó `setInputHeightForContent()`:
   - Calcula líneas visuales totales.
   - Limita contra `MaxHeight`.
   - Si todo el contenido cabe en el alto actual, re-aplica `SetValue(value)` y `CursorEnd()` después de `SetHeight` para resetear el viewport interno del textarea y evitar el `YOffset` obsoleto.

4. Se evitó que teclas normales usadas para escribir sean también procesadas por el viewport del transcript. El viewport ahora recibe updates solo para mensajes no-key, evitando scroll accidental del transcript mientras se escribe.

## Pruebas
Archivo: `internal/tui/chat_layout_test.go`.

Se agregaron regresiones para:

- Verificar que el autoresize del textarea no oculte la primera línea soft-wrapped.
- Verificar que el transcript se envuelva antes de enviarlo al viewport.
- Mantener la prueba de que `viewport + bottom chrome` coincide con la altura total de terminal.

Comandos ejecutados:

```bash
nix run nixpkgs#go -- test ./internal/tui
nix run nixpkgs#go -- test ./...
nix run nixpkgs#go -- build ./...
```

Resultado: todos pasan.

## Summary para commit
Corregir recorte del transcript al escribir con textarea auto-redimensionado.

## Description para commit
Centraliza el refresco del transcript con contenido envuelto antes de `GotoBottom`, resetea el viewport interno del textarea tras cambios de alto cuando el contenido cabe y evita que teclas de escritura desplacen el viewport del chat. Agrega pruebas de regresión para el autoresize soft-wrapped y el wrapping previo al viewport.
