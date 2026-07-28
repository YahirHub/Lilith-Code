# Tarea 05 — Cancelación instantánea y cambio de modelo

## Estado
Pendiente por revalidación tras la regresión reportada en Windows.

## Objetivo
- Hacer que Ctrl+C detenga inmediatamente el turno activo, incluidas las herramientas/procesos en ejecución, sin reanudar el agente por resultados tardíos.
- Garantizar que `/models` cambie la selección activa y que el modelo elegido se use desde la siguiente petición del usuario sin reiniciar la CLI.
- Mantener fijo el modelo/proveedor durante un mismo turno para que una continuación de tool calls no cambie de backend a mitad de la tarea.

## Criterios de aceptación
- Ctrl+C actualiza la UI al instante y no espera a que finalice Electron u otro proceso hijo.
- La cancelación llega al `context.Context` de `run_terminal_command` y al árbol de procesos.
- Un resultado de tool/API perteneciente a un turno cancelado se ignora y no puede reactivar el agente.
- Una cancelación se distingue de un error/timeout.
- Elegir otro modelo en `/models` se refleja en la siguiente solicitud iniciada por el usuario.
- Tool continuations del turno ya iniciado conservan el modelo con el que comenzó ese turno.
- Existen pruebas de regresión para el ciclo de cancelación y selección de modelo.

## Implementado
- [x] Contexto raíz compartido por proveedor y tools.
- [x] IDs de turno para descartar resultados tardíos.
- [x] Ctrl+C sin `Resize()` completo y panel de comando cancelado inmediatamente.
- [x] `run_terminal_command` recibe la cancelación del turno.
- [x] Shell diferencia cancelación de timeout y limita la espera posterior al hard kill.
- [x] Modelo/proveedor congelados durante un turno y nueva selección aplicada al siguiente.
- [x] `/models` actualiza memoria + disco sin volver a consultar catálogos remotos.
- [x] Pruebas de regresión añadidas.

## Validación realizada
- `go test ./internal/shell ./internal/tools` correcto en copia temporal con `go 1.23`, sin modificar el `go.mod` real.
- La suite TUI no puede ejecutarse en el sandbox porque las dependencias Charm no están cacheadas y la red hacia `proxy.golang.org` está bloqueada.

## Validación local pendiente
```powershell
go test ./...
go vet ./...
go run .\cmd\li\
```
