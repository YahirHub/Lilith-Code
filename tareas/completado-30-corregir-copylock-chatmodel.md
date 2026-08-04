# Tarea 30 — Corregir copia de ChatModel detectada por go vet

## Estado
Completada y verificada estáticamente.

## Objetivo
Evitar que el constructor de la pantalla de chat copie por valor `ChatModel`, que contiene `sync/atomic.Uint64` y por tanto no debe copiarse después de su primer uso.

## Alcance
- Hacer que `NewChat` construya y devuelva `*ChatModel`.
- Adaptar router y pruebas para conservar una única instancia persistente.
- Añadir una regresión de compilación/vet si corresponde.
- Actualizar contexto y documentación interna.

## Validación esperada
- `go vet -mod=readonly -tags=grammar_set_core ./internal/tui`
- `go test -mod=readonly -tags=grammar_set_core -count=1 ./internal/tui`
- `git diff --check`
