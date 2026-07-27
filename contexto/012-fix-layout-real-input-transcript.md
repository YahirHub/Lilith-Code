# 012 - Fix layout real input/transcript

## Problema

El bug persistía al escribir mensajes largos: parte de la última respuesta del CLI desaparecía o se corrompía visualmente en la zona superior del input.

## Causa

El cálculo anterior usaba `textarea.LineCount()`, pero en `bubbles/textarea` ese método sólo cuenta líneas lógicas del valor, no líneas visuales producidas por wrap automático. Además, `Resize` calculaba el alto del transcript antes de actualizar el ancho real del textarea.

## Cambio

- Se agregó `visualInputLineCount` para estimar líneas visibles según el ancho real de texto.
- `Resize` ahora actualiza primero el ancho del textarea, sincroniza su altura y luego calcula el alto del viewport.
- El alto reservado para los controles inferiores se calcula con la altura renderizada real de paleta, caja de entrada y barra de estado.
- `View` y `Resize` comparten los mismos helpers de render para evitar discrepancias de una línea.

## Pruebas

- `go test ./internal/tui`
- `go build ./...`

## Commit sugerido

Corregir reserva real de altura del input en la TUI