# Tarea 07 — Intercepción de escritura y cancelación definitiva

## Estado
pendiente

## Objetivo
Corregir las dos regresiones confirmadas en Windows:
1. impedir que un modelo intente una escritura completa sobre un archivo existente y hacer que reciba un redirect compacto hacia `str_replace`/`apply_diff` sin modificar el archivo;
2. garantizar que Ctrl+C invalide el turno de forma inmediata y que el cierre tardío de Electron u otra herramienta jamás pueda reanudar el agente.

## Referencia analizada
- Snapshot de pi.dev entregado por el usuario, mantenido sólo en `/mnt/data/lab/pi-current/pi-main` y excluido del proyecto/ZIP.
- `bash.ts`/`shell.ts`: AbortSignal por ejecución y `taskkill /F /T /PID` no bloqueante en Windows.
- `edit.ts`/`file-mutation-queue.ts`: validación sobre archivo actual y serialización por ruta real.
- La semántica overwrite de `write` de pi.dev NO se copia porque contradice la política solicitada para Lilith; se porta el patrón de intercepción previa y se conserva `create_file` como create-only.

## Criterios de aceptación
- `create_file` jamás sobrescribe un target existente.
- El schema de `create_file` anuncia `path` antes de `content` para permitir preflight temprano durante streaming.
- `write` y `write_file` no están expuestos en schemas, pero si un modelo los alucina se interceptan y reciben un redirect recuperable.
- Un alias `write/write_file` se corta apenas la tool call es reconocible; no necesita esperar a que termine el cuerpo.
- `tool_search` no materializa `create_file` ante búsquedas genéricas de write/edit; exige intención explícita de crear.
- Cada petición HTTP/SSE dentro de un turno tiene un `requestID`; eventos tardíos de un request cancelado no pueden afectar su reemplazo.
- Ctrl+C pone `activeTurnID` y `activeRequestID` a cero antes de propagar cancelación y limpia `Pensando/Trabajando` inmediatamente.
- Windows lanza `taskkill /F /T` de forma no bloqueante y `os/exec` limita a 100 ms la espera por pipes/proceso.
- Un resultado de herramienta o SSE posterior a Ctrl+C se descarta y no ejecuta `runTurn()`.

## Validación del sandbox
- `go test ./internal/tools` correcto en copia temporal Go 1.23 con stub local de `x/text`, omitiendo únicamente el test NFKC que requiere la dependencia real no cacheada.
- `go test ./internal/shell` correcto.
- `go vet ./internal/tools ./internal/shell` correcto.
- Cross-compile de tests de `internal/shell` para `windows/amd64` correcto.
- `go/parser` valida sintaxis del árbol Go completo.
- Suite TUI completa pendiente en el equipo del usuario porque el sandbox no puede descargar Go 1.24 ni las dependencias Charm.

## Corrección de compilación posterior
- Se corrigió la firma de `scanJSONString` de `(string, found, complete bool)` a `(string, bool, bool)`.
- La firma anterior era sintácticamente válida pero Go interpretaba los tres resultados como `bool`, causando los errores de tipo reportados por `go run`.
- Validación completa de `internal/tui` sigue pendiente en Windows/Go 1.24+ por falta de dependencias Charm en el sandbox.
