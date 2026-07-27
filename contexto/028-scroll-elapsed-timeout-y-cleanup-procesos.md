# 028 · Scrollbar del transcript, Elapsed estable, timeout por comando y limpieza real de procesos huérfanos

**Fecha**: 2026-07-27
**Estado**: aplicado

## Contexto

Tras el fix del box roto (027) el usuario reportó cuatro problemas encadenados
en el mismo flujo `run_terminal_command`:

1. El contador **Elapsed** en el panel del comando avanzaba a tirones — a veces
   subía "873ms → 1.4s" y otras se saltaba varios segundos.
2. No había forma de leer el historial hacia arriba: ni scrollbar visible ni
   soporte para `PgUp` / `PgDown`, y menos aún mientras Lilith estaba trabajando
   (el auto-scroll tiraba el viewport al fondo con cada frame).
3. La API interna soporta `timeout_seconds`, pero no estaba documentada en el
   system prompt, así que el modelo nunca lo mandaba.
4. Al pulsar `Ctrl+C` durante un comando largo, la UI decía "Tarea cancelada"
   pero los procesos hijos (`npm run dev`, `docker`, `tsc --watch`, …) seguían
   vivos en segundo plano. `exec.CommandContext` solo mataba el PID directo del
   shell.

## Cambios aplicados (Ponytail)

### `internal/shell/shell.go` + `procgroup_unix.go` / `procgroup_windows.go`

- El shell se lanza como líder de un **process group** propio (`Setpgid` en
  Unix, `CREATE_NEW_PROCESS_GROUP` en Windows).
- `cmd.Cancel` envía `SIGKILL` a todo el grupo (`kill(-pgid)` en Unix,
  `taskkill /T /F` en Windows).
- Se añade `cmd.WaitDelay = 2s` para no colgar el ciclo si algún hijo se
  resiste a morir.
- Con esto, `Ctrl+C` en la TUI o un timeout matan **todo el árbol** de
  procesos, no solo la shell.

### `internal/tui/chat.go`

- Nueva `cmdElapsedTickMsg` (500ms) que refresca el transcript mientras haya
  paneles de comando vivos o el turno esté en streaming. Elapsed ahora avanza
  1s cada segundo, sin depender de deltas del modelo.
- `humanizeDur` en `cmdpanel.go` redondea siempre a segundos enteros (formato
  `1s`, `12s`, `1m03s`, `1h05m`) para que el usuario no vea saltos "873ms →
  1.4s → 2s".
- Se añaden teclas de scroll (`PgUp`, `PgDown`, `Home`, `End`, `Shift+↑/↓`,
  `Ctrl+U/D/B/F`) que van directas al viewport **incluso durante streaming**.
- Flag `userScrolled`: cuando el usuario se separa del fondo, el auto-scroll
  queda pausado y la CLI ya puede escribir sin arrastrarlo de vuelta. Al
  enviar un turno nuevo o llegar al fondo con End, el flag se resetea.
- Se añade una **scrollbar vertical** (`renderScrollbar`, nuevo archivo
  `scrollbar.go`) a la derecha del transcript, con canal apagado y perilla
  primary. Tamaño y posición proporcionales al contenido.
- El prompt del sistema documenta ahora `timeout_seconds` con recomendaciones
  (30 por defecto, 120–300 para builds/tests) y aclara que expira el árbol
  entero.

## Verificación

- `go build ./...` OK.
- `go test ./internal/tui/... ./internal/shell/...` OK.
- Prueba manual: `sleep 300` desde `!` + Ctrl+C → el proceso `sleep`
  desaparece de `ps` inmediatamente (antes quedaba huérfano).

## Descripción sugerida para commit

**Título**: `tui/shell: scrollbar, elapsed estable, timeouts y cleanup de procesos`

**Cuerpo**:
El panel de `run_terminal_command` ahora avanza Elapsed a segundos enteros
con un tick propio de 500ms, así deja de saltar a tirones. Añade scrollbar
vertical con perilla, soporte de `PgUp/PgDown/Home/End/Shift+↑↓` y respeto
de la posición del usuario durante streaming (auto-scroll pausado mientras
esté leyendo arriba). Documenta `timeout_seconds` en el system prompt para
que el modelo lo use por comando. En `internal/shell` los comandos corren
ahora en su propio process group (Unix) o job (Windows) y `cmd.Cancel`
mata todo el árbol con `SIGKILL` / `taskkill /T`: `Ctrl+C` y timeouts ya no
dejan procesos huérfanos.