# 031 — Portar gaps restantes de pi.dev

## Summary
Portar features de pi.dev (packages/coding-agent/src/core/tools + harness) que
faltaban en Lilith: nueva tool `apply_diff`, `code_search` con context/literal/
ignore_case/limit/path, `list_directory` con `limit`, `read_files` detecta
imágenes y binarios, y un mutex por-archivo para serializar escrituras
concurrentes (equivalente a `file-mutation-queue`).

## Description
- **`apply_diff`** (nueva) — aplica diff unificado (`@@ -a,b +c,d @@`) a un
  archivo existente. Valida cada hunk contra el contenido actual, aplica todo
  o rechaza con error descriptivo. Usa mutex por path.
- **`code_search`** — nuevos params `literal`, `ignore_case`, `context`,
  `path`, `limit`. Truncado de líneas largas (500 chars) y límite total de
  matches. Prompt aclara todos los flags.
- **`list_directory`** — orden alfabético estable, `limit` (default 500) con
  nota de truncado.
- **`read_files`** — detecta `.png/.jpg/.gif/.webp/.bmp` y otros binarios; en
  lugar de volcar bytes al contexto, emite una nota `[image kind, N bytes]`
  o `[binary file, N bytes]`.
- **`write_file` / `str_replace` / `apply_diff`** — todas toman `lockFile()`
  antes del read-modify-write, evitando corrupción si el modelo emite calls
  paralelos al mismo archivo.
- **systemPrompt** — documenta `apply_diff` y los flags nuevos de
  `code_search`. `promptHints` ahora sugiere `apply_diff` cuando el prompt
  menciona rutas de archivo.

## Files
- `internal/tools/diff.go` (nuevo) — parser + applier de unified diff.
- `internal/tools/mutex.go` (nuevo) — `lockFile(path) *sync.Mutex`.
- `internal/tools/exec.go` — `code_search` reescrito + helper `boolArg`.
- `internal/tools/files.go` — `read_files` (image/binary), `list_directory`
  (limit), `write_file` + `str_replace` (mutex), helpers `imageKind`/`isBinary`.
- `internal/tools/registry.go` — hint para `apply_diff`.
- `internal/tui/chat.go` — systemPrompt actualizado.
- `internal/tools/pi_gap_test.go` (nuevo) — cubre apply_diff (ok + mismatch),
  read_files image y list_directory limit.

## Commit
Summary: Portar gaps de pi.dev (apply_diff, code_search flags, mutex, image detect)
Description: Nueva tool apply_diff (unified diff), code_search con context/literal/ignore_case/limit/path, list_directory con limit, read_files detecta binarios/imágenes, mutex por-archivo en escritores. systemPrompt y tests actualizados.
