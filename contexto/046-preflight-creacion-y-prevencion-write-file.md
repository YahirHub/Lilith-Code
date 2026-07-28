# Fecha
2026-07-27

# Objetivo
Evitar que los modelos gasten cientos o miles de tokens intentando reescribir archivos existentes con la herramienta de creación, y hacer que las tareas de edición reciban herramientas semánticamente correctas desde el primer request.

# Decisiones tomadas
- La herramienta pública `write_file` se reemplaza por `create_file` porque el nombre anterior induce a muchos modelos a asumir semántica de overwrite, como ocurre en otros coding agents.
- `write_file` no permanece en el registry ni en los schemas nuevos. La TUI conserva soporte visual para ese nombre únicamente al rehidratar sesiones históricas.
- `create_file` sigue siendo estrictamente create-only; nunca sobrescribe un target existente.
- El selector perezoso ya no incluye la herramienta de creación en cualquier prompt de escritura/edición. Sólo se activa ante una intención explícita de crear/agregar un archivo; si una implementación más amplia necesita un archivo nuevo, `tool_search` puede materializarla.
- Se añade preflight temprano durante streaming: cuando una tool call parcial de `create_file` ya contiene `path`, se comprueba el filesystem antes de esperar `content` completo.
- Si el target existe, se cancela únicamente el request HTTP/SSE actual mediante un contexto hijo, no el contexto raíz del turno. Se crea un tool result `FILE_EXISTS` protocolariamente válido y el agente continúa con herramientas de edición.
- Después de `FILE_EXISTS`, `create_file` se retira del set activo y se garantizan `read_files`, `str_replace` y `apply_diff`.
- El cuerpo rechazado no queda en el historial API ni ocupando el panel de la TUI.

# Arquitectura actual
Cada turno mantiene un `turnCtx` raíz cancelable por Ctrl+C. Cada llamada al proveedor crea ahora un contexto hijo con `requestCancel`. Esto permite abortar una generación incorrecta de `create_file` sin matar el turno ni las herramientas posteriores.

Flujo de edición esperado:
1. Petición de editar/corregir/refactorizar -> `read_files` / `str_replace` / `apply_diff`; `create_file` no se expone de forma automática.
2. Petición explícita de crear archivo -> `create_file` disponible.
3. Si durante streaming `path` apunta a un archivo existente -> preflight inmediato -> cancelar sólo el stream -> `FILE_EXISTS` compacto -> continuar con herramientas de edición.
4. Si el path no existe -> la generación continúa y `create_file` crea el archivo normalmente.

# Librerías usadas
Sólo standard library para el nuevo preflight/cancelación (`os`, `context`, `encoding/json`). No se agregaron dependencias.

# Archivos importantes modificados
- `internal/tools/files.go`
- `internal/tools/registry.go`
- `internal/tools/mutex.go`
- `internal/tools/tools_test.go`
- `internal/tui/chat.go`
- `internal/tui/filepanel.go`
- `internal/tui/chat_tools_test.go`
- `internal/tui/filepanel_test.go`
- `tareas/en-proceso-06-port-pi-edicion-y-prompt.md`

# Problemas encontrados
El guard anterior era correcto para integridad de datos pero tardío para consumo de tokens: `tools.Execute` sólo podía responder `FILE_EXISTS` después de que el proveedor hubiera terminado de generar el argumento `content`. Además, `write_file` era un nombre semánticamente contradictorio con la política local create-only y se exponía incluso en prompts de `edit/fix/modify`.

# Soluciones implementadas
- Nombre público inequívoco `create_file`.
- Selección perezosa más estricta.
- `PreflightCreateFile` reutilizable sin escrituras.
- Contexto hijo por request del proveedor.
- Intercepción de tool call parcial y cancelación temprana del stream cuando el path ya existe.
- Compactación y retirada de la herramienta después del primer `FILE_EXISTS`.
- Compatibilidad de render para `write_file` histórico.
- Pruebas para selección de herramientas, preflight y compactación.

# Pendientes
Validar en Windows con el proveedor/modelo usado en la captura que el stream entrega `path` antes de un cuerpo grande y que el panel pasa a `skipped` casi inmediatamente. En proveedores no-streaming no es posible recuperar tokens ya generados en servidor; la prevención depende principalmente del nuevo nombre y del selector.

# Próximos pasos
Ejecutar `go test ./...` y `go vet ./...` con Go 1.24+ en el equipo del usuario y repetir una tarea de rediseño sobre un CSS existente para confirmar que no se ofrece `create_file` o, si el modelo la materializa, que el preflight corta la generación antes del cuerpo completo.


# Corrección posterior
La prueba real mostró dos límites de esta versión: el modelo todavía podía generar gran parte de `content` antes de que `path` quedara disponible para preflight, y una cancelación local de request compartía sólo el ID de turno con la continuación. Ambos puntos quedan superados por `047-intercepcion-escritura-y-cancelacion-definitiva.md`: schema ordenado con `path` primero, aliases `write/write_file` interceptados apenas se conoce la tool call, ID independiente por request SSE y cancelación de árbol de procesos alineada con pi.dev.
