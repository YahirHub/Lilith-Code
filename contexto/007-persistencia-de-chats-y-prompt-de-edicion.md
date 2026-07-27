# 007 — Persistencia de chats y prompt de edición

# Fecha

2026-07-27

# Objetivo

1. Evitar que el modelo recreara desde cero un archivo existente cuando el
   usuario pedía una modificación puntual.
2. Guardar las conversaciones por proyecto y poder reanudarlas con
   `li --continue` o con `/history`, como en Codewolf.

# Decisiones tomadas

- El prompt de sistema pasa a incluir reglas de edición explícitas, adaptadas
  de `original/agents/base2/base2.ts` y de las descripciones de
  `original/common/src/tools/params/tool/{write-file,str-replace}.ts`:
  leer antes de editar, usar `str_replace` con el mínimo contexto único y
  reservar `write_file` para archivos nuevos.
- El prompt sigue siendo corto: las reglas viajan sólo cuando el turno lleva
  esquemas de herramientas activos (selección perezosa intacta).
- Además del prompt se añade una **guardia determinista**: `write_file`
  rechaza sobrescribir un archivo existente y no vacío que el modelo no haya
  leído en la sesión. El error devuelto le indica la ruta correcta
  (`read_files` + `str_replace`), así que el modelo se corrige solo.
- El seguimiento de archivos vistos vive en `tools.Env.Seen`, un conjunto
  propiedad del `ChatModel` (una sesión = un conjunto).
- Persistencia en `~/.li/projects/<carpeta>-<hash>/chats/<id>.json`. El hash
  corto de la ruta completa evita que dos proyectos con el mismo nombre de
  carpeta compartan historial (problema documentado en el original,
  `contexto/020-historial-global-proyectos.md`).
- Se guarda el historial real del modelo (`[]openai.Message`) como única
  fuente de verdad; el transcript visible se reconstruye al reanudar.
- Las conversaciones vacías no se guardan.

# Arquitectura

```text
internal/session
   Store{root: ~/.li/projects}
      ChatsDir/Save/Load/List/Latest/Delete
   Session{ID, Title, ProjectPath, CreatedAt, UpdatedAt, Messages}

ChatModel
   store, sess, project, seen
   persist()      → tras el mensaje del usuario y al cerrar cada turno
   LoadSession()  → reconstruye transcript + history

/history → HistoryModel → resumeSessionMsg → RootModel → chat.LoadSession
li --continue → session.Store.Latest(cwd) → AppContext.Resume
```

# Archivos importantes

- `internal/session/session.go`, `internal/session/session_test.go` (nuevos)
- `internal/tui/history_screen.go` (nuevo)
- `internal/tui/chat.go` (persistencia, `LoadSession`, prompt, `Env.Seen`)
- `internal/tui/app.go` (`AppContext.Resume`, `resumeSessionMsg`)
- `internal/tui/commands.go` (`/history`)
- `internal/tools/registry.go`, `internal/tools/files.go` (guardia de escritura)
- `cmd/li/main.go` (`--continue` / `-c`)

# Problemas encontrados y soluciones

- **Reescritura completa en vez de edición**: el prompt anterior no distinguía
  creación de edición y `write_file` no tenía freno. Solución: reglas de
  edición + guardia por archivo leído.
- **Historial por nombre de carpeta**: el original colisiona con carpetas
  homónimas. Solución: sufijo hash de la ruta completa.
- **Reanudar sin perder el contexto del modelo**: se persiste el historial
  completo, incluidos `tool_calls` y mensajes `role: tool`.

# Validación

- `nix run nixpkgs#go -- build ./...`, `-- vet ./...` y `-- test ./...`: todo
  correcto (nuevos tests de sesión y de la guardia de escritura).
- `gofmt` aplicado.

# Pendientes

- Vista global de historial entre proyectos (`Tab`), como en el original.
- Compactación de sesiones largas antes de reenviarlas al modelo.

# Próximos pasos

1. Probar `/history` con varias conversaciones y borrado con `Ctrl+D`.
2. Probar `li --continue` en un proyecto con historial y en otro sin él.
3. Pedir una edición sobre un archivo existente y confirmar que usa
   `str_replace`.