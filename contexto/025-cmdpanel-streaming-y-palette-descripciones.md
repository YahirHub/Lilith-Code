# 024 · CommandPanel streaming + palette con descripciones

## Descripción

Se agrega un `CommandPanel` estilo terminal para `run_terminal_command` con
tres estados de color: **azul** mientras el modelo aún está escribiendo el
comando o el proceso está corriendo, **verde** al salir con código 0 y
**rojo** cuando el exit code es distinto de cero o hubo timeout. El panel
muestra el `$ <comando>` en vivo (los deltas de argumentos se pintan al
instante igual que los `FilePanel` de `write_file`/`str_replace`), el
reloj `Elapsed 1.4s` que avanza con el `thinkingTick`, la salida real
(stdout/stderr) truncada a 10 líneas con hint `… N earlier lines
(ctrl+o to expand)`, y un footer `Command exited with code N`.

El `SuggestionMenu` (`/`) se rediseña a dos columnas alineadas (label en
primary/secondary + descripción en muted a la derecha) inspirado en la
paleta de Claude Code / pi.dev. Las skills aparecen como `skill:<nombre>`
en color secundario para distinguirlas de los comandos slash.

## Cambios

- `internal/tui/cmdpanel.go` (nuevo): tipo `CommandPanel`, `IsCommandTool`,
  parsing de `exit_code:` / `stdout:` / `stderr:` del formato de
  `tools/exec.go`, streaming vía `partialJSONString("command")`,
  render con border color por estado.
- `internal/tui/chat.go`:
  - Nueva `MessageKind` `MsgCommand` + campo `Command *CommandPanel`.
  - `ChatModel.cmdPanels` y `cmdByCall` para trackear paneles vivos.
  - `applyToolCalls` crea/actualiza el panel para `run_terminal_command`
    y marca como superseded cualquier panel abierto (file o cmd) al
    aparecer un nuevo `output_index`.
  - Al dispatch de `runTools` se llama `cp.Start()` para arrancar el
    reloj `Elapsed`.
  - `toolResultsMsg` ahora también resuelve por `cmdByCall` y llama
    `Finish` con el output del shell.
  - `LoadSession` rehidrata `CommandPanel` para sesiones reanudadas.
- `internal/tui/suggestion_menu.go`: layout 2 columnas alineadas,
  label skill diferenciado por color, ancho de descripción calculado
  contra el ancho de la caja.

## Sumary (commit-ready)

`feat(tui): live CommandPanel for run_terminal_command + aligned palette`

CommandPanel streams shell commands in real time (blue running, green on
exit 0, red on failure/timeout), shows stdout/stderr with a preview
window, and displays elapsed/exit-code footer. Slash palette redesigned
into a two-column aligned layout that separates skills from commands.
