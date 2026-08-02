> **Nota histórica:** `101-escritura-atomica-y-guard-heredoc.md` reemplaza la política de nombres descrita aquí. Actualmente sólo `write` es un alias ambiguo bloqueado; `write_file` y `append_file` son herramientas públicas y atómicas. Las decisiones sobre requestID, cancelación y `create_file` continúan vigentes.

# Fecha
2026-07-27

# Objetivo
Eliminar definitivamente dos fallos observados en pruebas reales de Lilith: generación desperdiciada al intentar escribir archivos existentes y reanudación del agente después de Ctrl+C cuando un proceso GUI (Electron) termina más tarde.

# Decisiones tomadas
- Mantener `create_file` como herramienta estrictamente create-only. Aunque pi.dev publica `write` con semántica de crear o sobrescribir, esa conducta no se porta porque el usuario pidió bloquear sobrescrituras completas accidentales.
- Portar de pi.dev el patrón de intercepción previa: llamadas incompatibles se convierten en resultados de política recuperables antes de ejecutar una mutación.
- `write` y `write_file` se consideran aliases alucinados/legacy: nunca se exponen en schemas y nunca escriben. Si aparecen, se devuelven `FILE_EXISTS`, `USE_CREATE_FILE` o `WRITE_BLOCKED` según lo que se conozca del path.
- Durante streaming, `write/write_file` se bloquean en cuanto se conoce nombre + call ID, incluso si `path` aún no terminó. `create_file` sólo se corta cuando el string `path` está completo y el target ya existe.
- El schema de `create_file` usa structs ordenados para serializar `path` antes de `content`, aumentando la probabilidad de que el preflight ocurra antes de generar un cuerpo grande.
- `tool_search` sólo materializa `create_file` si la búsqueda expresa creación explícita; consultas genéricas como `write file` no la activan.
- Cada request al proveedor dentro de un turno obtiene `requestID` independiente del `turnID`. Es obligatorio para distinguir una conexión SSE cancelada localmente de la continuación que empieza inmediatamente dentro del mismo turno.
- Ctrl+C invalida primero `activeTurnID` y `activeRequestID`, después cancela contextos. Esto hace que cualquier evento ya encolado quede obsoleto antes de que el SO termine de cerrar procesos.
- En Windows se adopta el patrón de pi.dev: lanzar `taskkill /F /T /PID` sin esperar a que `taskkill` termine. `WaitDelay` queda en 100 ms como red de seguridad para el shell/pipes.
- Los locks de mutación existentes se canonicalizan con `EvalSymlinks` para que aliases de una misma ruta real compartan exclusión, siguiendo el enfoque realpath de pi.dev.

# Arquitectura actual
## Política de archivos
1. Archivo nuevo explícito -> `create_file`.
2. Archivo existente -> `str_replace` o `apply_diff`.
3. `create_file` + target existente -> preflight `FILE_EXISTS`, cuerpo compactado, retirada de `create_file` del resto del turno y activación de editores.
4. `write/write_file` alucinado -> se intercepta sin ejecutar; si no hay path completo todavía se corta igualmente con `WRITE_BLOCKED` y se ofrecen las herramientas válidas.
5. `str_replace` y `apply_diff` leen/validan el archivo actual dentro del lock de mutación; no dependen de un flag de lectura previa.

## Ciclo de turno/request
- `turnID`: identidad del turno completo del usuario (LLM + tools + continuaciones).
- `requestID`: identidad de una llamada concreta HTTP/SSE dentro del turno.
- Un mensaje de stream se procesa sólo si ambos IDs coinciden y `turnCtx` sigue vivo.
- Ctrl+C pone ambos IDs activos a cero antes de llamar a `cancel()`.
- Los resultados de tools sólo se aceptan si el `turnID` sigue activo y el contexto no está cancelado.

## Cancelación de procesos
- Linux/macOS: grupo de procesos propio + SIGKILL al PGID.
- Windows: grupo de procesos propio + `taskkill /F /T /PID` lanzado de forma asíncrona; fallback al proceso directo si taskkill no puede iniciarse.
- `exec.Cmd.WaitDelay = 100ms` evita esperar por handles heredados después de cancelar.

# Librerías usadas
No se añadieron dependencias. Se usa standard library (`context`, `os/exec`, `syscall`, `filepath`, `encoding/json`). La dependencia `golang.org/x/text` ya pertenecía al trabajo previo de matching NFKC.

# Archivos importantes modificados
- `internal/tui/chat.go`
- `internal/tui/filepanel.go`
- `internal/tui/chat_cancel_model_test.go`
- `internal/tui/chat_streaming_input_test.go`
- `internal/tui/chat_tools_test.go`
- `internal/tui/chat_test_helpers_test.go`
- `internal/tools/files.go`
- `internal/tools/registry.go`
- `internal/tools/mutex.go`
- `internal/tools/mutex_test.go`
- `internal/tools/write_policy_test.go`
- `internal/shell/shell.go`
- `internal/shell/procgroup_windows.go`
- `internal/shell/shell_test.go`

# Problemas encontrados
- El preflight previo podía llegar tarde si el proveedor/modelo emitía `content` antes de `path`; además el schema basado en `map[string]any` se serializaba con claves ordenadas alfabéticamente, colocando `content` antes de `path`.
- `partialJSONString` servía para preview y devolvía strings incompletos; usarlo para decisiones de filesystem podía confundir un prefijo parcial con una ruta existente.
- Una cancelación local de request y su continuación compartían únicamente `turnID`, por lo que un `context.Canceled`/EOF tardío de la conexión vieja podía interferir con el request nuevo del mismo turno.
- Los guards anteriores sólo descartaban IDs no-cero que no coincidían (`id != 0 && id != active`); un evento asíncrono con `turnID == 0` podía atravesar la validación. Ahora cero también es inválido.
- En Windows el kill anterior ejecutaba `taskkill.Run()` de forma síncrona, haciendo que la cancelación dependiera de cuánto tardara Windows en cerrar descendientes de Electron/Node.
- `tool_search` podía volver a materializar `create_file` mediante consultas genéricas relacionadas con escritura.

# Soluciones implementadas
- Schema ordenado `path` -> `content` para `create_file`.
- Extractor `completeJSONString` separado del extractor parcial de preview.
- Intercepción inmediata de aliases `write/write_file` y compactación del payload rechazado.
- Redirects de política no tratados como errores fatales: `FILE_EXISTS`, `USE_CREATE_FILE`, `WRITE_BLOCKED`.
- Reglas de prompt que prohíben aliases legacy y obligan a seguir el redirect recibido.
- Filtro de `tool_search` para creación explícita.
- IDs por request SSE y guards estrictos en `Update`.
- Invalidación de IDs antes de la cancelación de contextos.
- `taskkill` asíncrono en Windows + `WaitDelay` de 100 ms.
- Canonicalización de locks de mutación por ruta real.
- Pruebas de regresión para redirects, schema, búsqueda de tools, path parcial, stale requests y cancelación.

# Pendientes
Validar en Windows con el proveedor real que:
- una llamada accidental a `create_file` sobre un CSS existente se corta apenas termina `path` y no acumula cientos de líneas;
- un `write/write_file` alucinado se corta incluso antes de tener path completo;
- al ejecutar Electron, Ctrl+C borra `Pensando/Trabajando` de inmediato, el proceso se termina y cerrar cualquier ventana superviviente no produce una nueva petición.

# Próximos pasos
Ejecutar `go test ./...`, `go vet ./...` y una prueba manual de Electron en Windows con Go 1.24+. Si esas pruebas pasan, marcar tareas 05, 06 y 07 según corresponda y preparar el siguiente commit funcional.
