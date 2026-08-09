# 139. Shell portátil propia sin mvdan

## Objetivo

Eliminar `mvdan.cc/sh/v3` y su aviso BSD-3-Clause sin perder el fallback de
shell que permite a Lilith ejecutar automatización portable cuando Bash o `sh`
no están disponibles.

## Nuevo módulo

Se crea `github.com/YahirHub/go-portable-shell`, un repositorio independiente
con estas propiedades:

- implementación original en Go puro;
- cero dependencias de runtime;
- compatible con `CGO_ENABLED=0`;
- licencia 0BSD, sin obligación de atribución downstream;
- API de handlers para comandos propios de la aplicación;
- estados de salida tipados y cancelación por `context.Context`;
- CI para Go 1.24/1.25, race, vet, Windows y Android ARM64.

## Lenguaje soportado

El motor implementa un subconjunto Bash/POSIX no interactivo y explícito:

- palabras, comillas, escapes, comentarios y continuaciones de línea;
- variables, asignaciones, parámetros posicionales y operadores `${VAR:-x}`;
- sustitución de comandos con límite de salida y profundidad;
- expansión aritmética, tilde y globbing;
- listas, `&&`, `||`, `!`, pipelines y `pipefail`;
- redirecciones de entrada/salida, append y duplicación `2>&1`;
- `if`, `while`, `until`, `for`, grupos, subshells y funciones;
- builtins de flujo, entorno, directorio, formato, pruebas y lectura;
- resolución de ejecutables externos con ambiente/directorio controlados.

Heredocs, job control, process substitution, arrays y extensiones Bash fuera del
contrato se rechazan. El motor no se presenta como Bash completo ni como un
sandbox.

## Integración con Lilith

`internal/shell/portable.go` usa el runner del nuevo módulo. Los fallbacks `rg`,
`grep`, `find`, `ls`, `cat`, `head`, `tail`, `wc`, `mkdir`, `touch`, `cp`, `mv`,
`rm`, `chmod` y `sha256sum` permanecen en Lilith y se conectan mediante
`portablesh.CommandHandler`.

Los ejecutables instalados continúan teniendo prioridad, salvo el nombre simple
`find` en Windows, donde se conserva el fallback POSIX para no delegar por error
a `System32\\find.exe`.

## Dependencias y avisos

- Se elimina `mvdan.cc/sh/v3 v3.13.1` de `go.mod` y `go.sum`.
- Se añade `github.com/YahirHub/go-portable-shell v0.1.0` como dependencia directa.
- Se elimina `THIRD_PARTY_NOTICES.md`, cuyo único contenido era la licencia de
  mvdan y quedó sin objeto al retirar ese código.
- `internal/imageocr/NOTICE.md` sigue siendo un aviso distinto y necesario; no
  está relacionado con el shell.

## Compatibilidad

- Shells nativas siguen por delante de `portable`.
- La selección automática y explícita conserva `shell=portable`.
- El binario continúa siendo Go puro y compatible con Linux, Windows y Termux.
- El toolbox y sus límites de salida/lectura no cambian.
- La documentación deja claro el alcance acotado y el fallo explícito de
  sintaxis no soportada.

## Publicación

- Repositorio público: `https://github.com/YahirHub/go-portable-shell`.
- Versión consumida por Lilith: `v0.1.0`.
- Commit publicado: `e867c4ff3e00d33609c003c3a8684b42810f1e22`.
- Checksums resueltos desde el repositorio publicado y registrados en `go.sum`;
  la integración no depende de un `replace` ni de una copia local.

## Validación ejecutada

En `go-portable-shell` se ejecutaron con Go 1.25.12:

- `go test ./...` y `go test -race ./...`;
- `go vet ./...` y `CGO_ENABLED=0 go build ./...`;
- compilación de pruebas para Windows AMD64 y Android ARM64;
- fuzzing del parser durante dos segundos, sin pánicos.

En Lilith, resolviendo `v0.1.0` desde el repositorio público:

- `go mod tidy -diff` y `go mod verify`;
- `go test -mod=readonly -tags=grammar_set_core ./...`;
- `go test -race -mod=readonly -tags=grammar_set_core ./...`;
- `go vet -mod=readonly -tags=grammar_set_core ./...`;
- compilación estática Linux AMD64 y compilación cruzada Windows AMD64;
- compilación y carga de las pruebas para Android ARM64 mediante `-exec=true`.

La ejecución nativa en Windows y en un dispositivo Android no se realizó en
este entorno Linux; las validaciones de esos destinos fueron cruzadas.
