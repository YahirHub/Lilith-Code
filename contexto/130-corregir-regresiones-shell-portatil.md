# 130. Corregir regresiones detectadas en la auditoría de shell portátil

## Motivo

La auditoría real en Windows del commit `7cb8a4d` confirmó que la arquitectura
principal funciona: `shell=portable`, pipes, redirecciones, comandos externos,
`code_search` con ripgrep y el backend Go sin ripgrep. También encontró fallos
acotados que impedían que la suite quedara limpia.

## Correcciones

### `rg` portátil

`portableRG` ya parseaba correctamente el patrón recibido, pero no lo copiaba a
`textsearch.Options.Pattern`. Como consecuencia, al no existir un `rg` real en
`PATH`, el fallback interno terminaba siempre con `rg: empty pattern`.

Ahora el patrón validado se asigna a `opts.Pattern` antes de ejecutar cualquier
target. Se conserva el límite global entre múltiples targets, `--hidden`, globs,
contexto y el resto del contrato existente.

### Colisión de `find.exe` en Windows

Windows incluye `System32\\find.exe`, cuyo contrato busca texto dentro de entrada y
no implementa el `find` POSIX. La shell portátil resolvía primero cualquier
binario de `PATH`, por lo que `find . -name "*.go"` terminaba delegándose al
comando incompatible de Windows.

Cuando se usa explícitamente `shell=portable`, el nombre simple `find` usa ahora
el fallback Go en Windows. Un ejecutable externo todavía puede invocarse mediante
una ruta explícita. El resto de comandos conserva la prioridad del ejecutable
nativo cuando existe.

### Schema de terminal

La descripción de `run_terminal_command` vuelve a contener literalmente
`CMD syntax`, tal como exige su prueba de contrato, sin cambiar la selección de
shell ni sus prioridades.

### Prueba de autocopia

La implementación rechaza correctamente `cp -r` hacia una subcarpeta del mismo
origen con el mensaje `cannot copy a directory into itself`. La prueba esperaba
por error `inside itself`; se alinea con el mensaje real.

### `go.mod` y `go.sum`

Se sincronizan los manifiestos con el grafo introducido por `mvdan.cc/sh/v3`:
se eliminan dos requisitos indirectos que ya no pertenecen al grafo (`pdf` y
`pixelmatch`) y se añaden los checksums `go.mod` transitivos que faltaban. Esto
corrige el fallo previo de `go mod tidy -diff` reportado por la auditoría.

## Pruebas de regresión

Además de las pruebas existentes de `rg` portátil, límite global, ocultos,
cancelación y toolbox, se añade una prueba Windows para impedir que
`System32\\find.exe` vuelva a interceptar la sintaxis POSIX dentro de
`shell=portable`.

## Compatibilidad

- No se cambia la prioridad automática de PowerShell, CMD, Bash o `sh`.
- `code_search` conserva ripgrep como acelerador y el motor Go como fallback.
- No se añaden ejecutables embebidos ni CGO.
- Git, Go, npm, Docker, Make y demás comandos continúan siendo externos.


## Versión de release

La versión se eleva de `0.3.0` a `0.3.1` porque `v0.3.0` ya existe como
release/tag publicado. `internal/version/version.go` permanece como fuente única
de verdad para el workflow de publicación, y los ejemplos de instalación de una
versión concreta se actualizan a `0.3.1`.
