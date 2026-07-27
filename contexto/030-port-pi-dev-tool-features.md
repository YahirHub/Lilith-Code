# 030 · Portar features de las herramientas de pi.dev

## Contexto
El usuario pidió analizar las herramientas de referencia de pi.dev
(`packages/agent/src/harness/tools/*.ts`) e implementar todas las que
faltaran en Lilith. El catálogo de pi expone: `read`, `write`, `edit`,
`bash` (más utilidades de imagen y cola de mutación por archivo).

Lilith ya tenía equivalentes de `read`, `write`, `edit` y `bash`
(`read_files`, `write_file`, `str_replace`, `run_terminal_command`),
pero con capacidades reducidas frente a pi. En vez de duplicar
herramientas se enriquecieron las existentes conservando compatibilidad
hacia atrás y siguiendo la metodología "Ponytail" (cambios mínimos y
enfocados).

## Cambios

### `internal/tools/files.go`
- **`read_files`** ahora acepta `offset` (línea 1-indexed) y `limit`
  (nº máx. de líneas). El resultado indica `showing lines A-B of N` y
  el próximo `offset` cuando queda contenido. Portado de pi
  (`read.ts`).
- **`str_replace`** admite un array `edits: [{old, new}, ...]` para
  aplicar varias sustituciones no solapadas en una sola llamada
  (`old`/`new` sueltos siguen funcionando). Se calculan todos los
  matches contra el contenido ORIGINAL, se rechazan solapamientos y se
  añade un fallback de _fuzzy matching_ que normaliza comillas
  tipográficas, guiones Unicode, espacios especiales y whitespace
  trailing, igual que `edit-diff.ts` de pi.
- Nuevos helpers privados: `sliceLines`, `collectEdits`, `applyEdits`,
  `locateOriginalSpan`, `normalizeFuzzy`.

### `internal/tools/exec.go`
- **`run_terminal_command`** tail-trunca `stdout`/`stderr` a las
  últimas 200 líneas / 32 KiB por stream. Cuando trunca, vuelca el
  stream completo en `$TMPDIR/lilith-exec-logs/*.log` y añade una nota
  con la ruta para que el modelo pueda inspeccionarlo con
  `read_files`. Portado de `bash.ts` de pi.
- Constantes `bashOutputMaxLines` / `bashOutputMaxBytes` y helpers
  `tailTruncate` / `writeFullLog`.

### `internal/tui/chat.go`
- Actualizado `systemPrompt` para documentar `offset`/`limit` en
  `read_files`, el modo multi-edit + fuzzy de `str_replace` y el
  tail-truncate con log completo de `run_terminal_command`.

### Tests
- `internal/tools/pi_features_test.go` cubre:
  1. paginación por líneas en `read_files`;
  2. `str_replace` con `edits[]` y match fuzzy sobre comillas
     tipográficas;
  3. rechazo de `old` vacío (regresión de 029).

## Verificación
- `go build ./...` ✓
- `go test ./internal/tools/...` ✓ (incluye los nuevos tests).

## Commit propuesto
**Summary:** Portar features de pi.dev (paginación, multi-edit + fuzzy,
tail bash log)

**Description:** `read_files` soporta `offset`/`limit`; `str_replace`
acepta `edits[]` con fuzzy matching Unicode y rechaza `old` vacío;
`run_terminal_command` tail-trunca stdout/stderr y guarda el stream
completo en un log temporal para inspección posterior. Prompt de
sistema actualizado y tests añadidos.
