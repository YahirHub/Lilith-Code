> **Actualización 2026-07-27:** la obligación de `read_files`/`Env.Seen` antes de editar quedó reemplazada por la arquitectura documentada en `045-port-pi-edicion-y-prompt.md`. Las herramientas de edición validan el contenido actual en disco en el momento de ejecutar.

# Fecha
2026-07-27

# Objetivo
Eliminar el error recurrente `error: 'old' must not be empty` al usar
`str_replace`, mejorando el prompt (descripciones de tool + system prompt)
en inglés para guiar mejor al modelo.

# Decisiones tomadas
- Descripción del tool `str_replace` reescrita en inglés con requisitos
  numerados: archivo existente + leído, `old` no vacío y copiado byte a
  byte, unicidad, `new` puede ir vacío, y patrón de inserción usando un
  ancla existente en el archivo.
- Descripciones de parámetros `path`/`old`/`new` amplificadas con las
  mismas reglas para que el schema JSON las lleve al modelo.
- `write_file` describe explícitamente que sirve solo para archivos NUEVOS
  (o rewrite explícito) y remite a `str_replace` para edición.
- Mensajes de error del runtime ahora son accionables: explican por qué
  falló y qué hacer en el siguiente intento (releer, expandir ancla,
  cambiar a `write_file`).
- Bullet de `str_replace` en `systemPrompt` de `internal/tui/chat.go`
  ampliado con las mismas reglas y la regla dura #3 refuerza que `old`
  nunca puede quedar vacío.

# Arquitectura actual
Sin cambios. Solo prompt engineering en:
- `internal/tools/files.go` (registro de write_file y str_replace)
- `internal/tui/chat.go` (`systemPrompt`)

Referencia consultada: pi.dev
(`packages/agent/src/harness/tools/edit.ts`), que usa el mismo enfoque
"oldText debe ser único y no solapado".

# Librerías usadas
Sin cambios.

# Archivos importantes modificados
- internal/tools/files.go
- internal/tui/chat.go

# Problemas encontrados
El modelo (Gemini 3.0 flash free vía OpenCode-like flow) llamaba a
`str_replace` con `old=""`, típicamente cuando quería crear/insertar
contenido. El schema previo no lo prohibía y el error no explicaba la
salida.

# Soluciones implementadas
Prompt + descripciones + error messages accionables. Sin cambios de
runtime salvo un chequeo extra `old == new` para evitar no-ops.

# Pendientes
- Observar en logs si aún hay reintentos con `old=""`; si el modelo lo
  sigue haciendo, valorar un helper `insert_at_anchor` explícito.

# Próximos pasos
- Correr `go build ./... && go test ./...` en Windows antes de release.
- Commit sugerido:
  Summary: Mejorar prompt de str_replace y write_file
  Description: Aclara reglas de uso, prohíbe old vacío y añade errores accionables para reducir reintentos fallidos.
