# Fecha

2026-08-04

# Objetivo

Corregir el fallo de `go vet` que detectaba una copia por valor de `ChatModel` al retornar desde `NewChat`. El modelo contiene `sync/atomic.Uint64`, cuyo marcador interno `noCopy` prohíbe copiarlo después de usarlo.

# Decisiones tomadas

- `NewChat` devuelve `*ChatModel` en lugar de `ChatModel`.
- El router raíz conserva exactamente ese mismo puntero como chat persistente y pantalla activa.
- Las pruebas y pantallas auxiliares reciben el puntero existente, sin crear dobles punteros.
- No se reemplazó `atomic.Uint64` por un contador no atómico ni se silenció `go vet`.

# Arquitectura actual

`RootModel` mantiene `chat *ChatModel`. El constructor crea el modelo directamente en memoria dinámica, inicializa su generación atómica y entrega el mismo puntero al router, comandos, rewind, forks y pruebas. Los métodos de `ChatModel` continúan usando receptores por puntero.

# Librerías usadas

No se agregaron ni actualizaron dependencias.

# Archivos importantes modificados

- `internal/tui/chat.go`
- `internal/tui/app.go`
- `internal/tui/chat_pointer_test.go`
- pruebas de chat, rewind y fork que usaban `&modelo`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`

# Problemas encontrados

`NewChat` construía un `ChatModel`, ejecutaba `m.agentGeneration.Store(1)` y después lo retornaba por valor. Esa devolución copiaba un `atomic.Uint64` ya usado y activaba el analizador `copylocks` de `go vet`.

# Soluciones implementadas

- Construcción con `m := &ChatModel{...}`.
- Retorno directo del puntero.
- Eliminación de direcciones redundantes en consumidores y pruebas.
- Prueba de regresión que exige que `NewChat` sea asignable a `*ChatModel` y que `NewRootModel` conserve la misma instancia persistente.

# Pendientes

- Ejecutar el workflow de release con Go 1.24 para confirmar la suite completa, `go vet ./...` y los builds cruzados.

# Próximos pasos

Volver a ejecutar manualmente **Publicar release**. La versión continúa siendo `0.2.1` porque el workflow anterior falló antes de crear el tag o release.
