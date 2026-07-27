---
name: Fix — el box del CommandPanel se rompía con salida real
description: Sanitiza stdout/stderr (ANSI, CR, tabs, control chars) y usa truncado por columnas para que el borde del panel de comandos no se parta con colores, progress bars o caracteres anchos.
type: fix
---

# 027 · Fix del box del CommandPanel roto por salida ANSI

## Fecha
2026-07-27

## Objetivo
Que el panel bash-style de `run_terminal_command` mantenga su borde
redondeado intacto sin importar qué imprima el proceso hijo.

## Problema
En los screenshots 134 y 135 el box del CommandPanel se "partía": el
borde inferior salía descuadrado y aparecían restos ANSI visibles.

### Causa
`shell.Run` captura `stdout`/`stderr` tal cual los emite el proceso. Eso
suele traer:

- Secuencias ANSI/CSI/OSC (colores, cursor moves).
- `\r` sueltos de barras de progreso que hacen que la terminal
  sobrescriba el borde derecho.
- Tabs (`\t`), backspaces y otros control chars C0 que lipgloss mide
  como ancho 0 y por eso las líneas quedaban más anchas que el interior
  de la caja.
- Caracteres wide (emoji, CJK) que la función `clip` recortaba por
  runas, no por columnas visibles.

## Cambios

### `internal/tui/output_sanitize.go` (nuevo)
- `sanitizeOutput(s)` limpia la salida antes de pintarla:
  - `ansi.Strip` para escapes.
  - `\r\n` y `\r` → `\n` (progress bars → último frame).
  - Tabs → 4 espacios.
  - Descarta otros control chars C0 (BS, BEL, VT, FF, NUL, DEL).
- `clipCols(s, cols)` recorta con `ansi.StringWidth`/`ansi.Truncate`,
  o sea por columnas reales, no por runas.

### `internal/tui/cmdpanel.go`
- `Finish()` guarda `Stdout`/`Stderr` pasados por `sanitizeOutput`.
- El header `$ <cmd>  <tag>` mide el ancho real del chip con
  `lipgloss.Width` y usa `clipCols` en vez del mágico `inner-14`.
- `renderOutput()` recorta cada línea con `clipCols(inner)`.

## Cómo lo hacen otros CLIs (referencia)
- **pi.dev / opencode**: capturan salida con PTY, parsean VT100 y
  renderean solo el estado final; nada llega crudo al framebuffer.
- **openclaude / claude-code**: strip agresivo de ANSI/CR/tab y trunco
  por columnas. Es la ruta ligera y es la que aplicamos (Ponytail: no
  necesitamos un intérprete VT100 completo aún).

Cuando el proyecto quiera hospedar TUIs interactivas (vim, htop,
progress fino) el siguiente paso es alojar el comando en un PTY con
`github.com/creack/pty` y correr un mini emulador VT sobre esa salida.

## Archivos modificados
- `internal/tui/output_sanitize.go` (nuevo)
- `internal/tui/cmdpanel.go`

## Pruebas
- `go build ./...`
- `go test ./internal/tui/...`

## Commit

Summary:
Sanear salida del CommandPanel y truncar por columnas

Description:
Elimina secuencias ANSI, `\r`, tabs y control chars antes de pintar
stdout/stderr en el panel bash-style de `run_terminal_command`. Añade
`clipCols` basado en `ansi.StringWidth`/`ansi.Truncate` para que
caracteres anchos (emoji, CJK) o barras de progreso ya no rompan el
borde. Además renumera archivos de contexto duplicados (017/024) para
mantener la cronología continua.

## Riesgos
- Al descartar control chars perdemos algunos efectos (colores,
  spinners); si el usuario los quiere, el siguiente paso es correr en
  PTY.
- `clipCols` depende de anchos POSIX estándar para wide runes;
  suficiente para las pruebas actuales.

## Próximos pasos
- Evaluar `creack/pty` + emulador VT mínimo para comandos
  interactivos.
- Añadir tests para `sanitizeOutput` (progress bar con `\r`, colores
  ANSI, CJK).
