# Fecha

2026-08-04

# Objetivo

Evitar que `str_replace` entre en ciclos de error cuando un modelo OpenAI-compatible usa nombres de argumentos aprendidos de otros agentes o interpreta como opcionales los campos de edición.

# Decisiones tomadas

- Mantener `old`/`new` como vocabulario canónico del schema.
- Exigir mediante JSON Schema `path` y, además, un par completo `old`/`new` o un `edits[]` no vacío.
- Declarar `minLength: 1` para cada target `old` y `minItems: 1` para el lote.
- Normalizar en runtime los alias comunes `old_string`/`new_string`, `oldText`/`newText`, `old_text`/`new_text`, `oldString`/`newString`, `search`/`replace` y sus variantes `search_*`/`replace_*`.
- Aplicar la misma normalización al par simple, a cada objeto de `edits[]` y al panel TUI.
- Rechazar la ausencia de un campo de reemplazo. Sólo una cadena vacía enviada explícitamente representa una eliminación.
- Mantener matching exacto/fuzzy, unicidad, no solapamiento, escritura atómica y preservación de BOM/CRLF sin cambios.
- Elevar la versión a `0.2.3`.

# Arquitectura actual

`internal/tools/files.go` conserva la validación y ejecución de `str_replace`. El schema dirige a los modelos hacia `old`/`new`, mientras una capa de compatibilidad local normaliza vocabularios de Claude/Pi y proveedores similares antes de construir los pares de edición.

`internal/tui/filepanel.go` reconoce los mismos aliases durante streaming y al decodificar `edits[]`, por lo que el panel muestra las líneas añadidas/eliminadas en vez de quedar en `+0`.

`internal/tui/context_prepare.go` compacta también esos nombres compatibles cuando una llamada antigua contiene targets o reemplazos extensos.

# Librerías usadas

No se agregaron ni actualizaron dependencias.

# Archivos importantes modificados

- `internal/tools/files.go`
- `internal/tools/pi_features_test.go`
- `internal/tui/filepanel.go`
- `internal/tui/filepanel_test.go`
- `internal/tui/context_prepare.go`
- `internal/tui/context_prepare_test.go`
- `internal/tui/chat.go`
- `internal/tui/chat_tools_test.go`
- `internal/version/version.go`
- `install.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `contexto/115-compatibilidad-argumentos-str-replace.md`
- `tareas/completado-32-normalizar-argumentos-str-replace.md`

# Problemas encontrados

- El schema anterior sólo marcaba `path` como obligatorio. Algunos modelos enviaban únicamente la ruta, aunque verbalmente ya hubieran identificado el cambio.
- Modelos entrenados con herramientas tipo Claude podían enviar `old_string`/`new_string`; Lilith sólo reconocía `old`, `new`, `oldText` y `newText`.
- La TUI tampoco reconocía esos aliases, por lo que mostraba `+0` aunque el proveedor hubiera emitido el contenido de edición.
- Si `old` existía pero `new` se omitía, el runtime interpretaba la ausencia como cadena vacía y podía convertir un argumento incompleto en una eliminación.

# Soluciones implementadas

- Schema condicional y restricciones de tamaño mínimo.
- Normalización de aliases conocidos sin relajar la regla de target no vacío.
- Error diagnóstico que enumera únicamente los nombres de campos recibidos, sin registrar su contenido.
- Reemplazo obligatorio explícito para impedir borrados accidentales.
- Preview y compactación histórica compatibles con los mismos nombres.
- Pruebas de runtime, schema, seguridad de borrado, `edits[]`, panel TUI y compactación.

# Pendientes

- Ejecutar `test.cmd -Vet` en Windows con Go 1.24 y dependencias oficiales.
- Repetir en una sesión real el cambio mostrado en las capturas usando CommandCodeL/OpenCode Free.

# Próximos pasos

1. Ejecutar la suite completa en Windows.
2. Pedir a Lilith una edición pequeña sobre un archivo existente y confirmar que el panel muestre un diff distinto de `+0`.
3. Publicar `v0.2.3` si la validación completa pasa.
