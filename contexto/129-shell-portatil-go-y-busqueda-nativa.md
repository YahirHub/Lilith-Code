# 129. Shell portátil en Go y búsqueda nativa sin `ripgrep`

## Objetivo

Permitir que Lilith ejecute sintaxis Bash/POSIX y busque dentro de repositorios
en instalaciones mínimas, sin convertir Bash, BusyBox o ripgrep en requisitos
de runtime y sin perder el binario estático construido con `CGO_ENABLED=0`.

La compatibilidad existente con PowerShell, CMD, Bash, `sh`, Git Bash y las
herramientas administradas se conserva. La nueva capa es un respaldo, no un
reemplazo agresivo del entorno del usuario.

## Arquitectura

### Resolución de shell

`run_terminal_command` mantiene esta prioridad:

- Windows: PowerShell para comandos neutrales, CMD para sintaxis CMD y Bash/sh
  para sintaxis POSIX cuando estén instalados.
- Linux, macOS y Termux: Bash y después `sh`.
- En cualquier host sin una shell POSIX compatible: `portable`.

El usuario también puede pedirla expresamente con `shell=portable`. El resultado
identifica la implementación como `embedded:mvdan.cc/sh/v3`.

La shell portátil se basa en `mvdan.cc/sh/v3` y ejecuta programas no interactivos
parseados en modo Bash. Conserva variables, expansión, pipes, redirecciones,
sustituciones, funciones, condicionales y bucles. Los ejecutables reales
encontrados en `PATH` siempre tienen prioridad. No pretende reproducir todos los
detalles internos de Bash ni semánticas Unix como procesos/PID/fork reales; los
comandos que dependan de esas diferencias deben seguir usando una shell nativa.

### Toolbox Go

Cuando un programa no está en `PATH`, la shell portátil puede resolver un
subconjunto deliberadamente acotado:

- búsqueda: `rg`, `ripgrep`, `grep`, `find`;
- lectura/listado: `ls`, `cat`, `head`, `tail`, `wc`;
- archivos: `mkdir`, `touch`, `cp`, `mv`, `rm`, `chmod`;
- integridad: `sha256sum`.

Los fallbacks tienen cancelación por contexto, límites de memoria/salida y
opciones explícitas. No pretenden clonar GNU coreutils. Un flag no soportado se
rechaza en vez de reinterpretarse silenciosamente.

Git, Go, npm, Docker, Make y cualquier otro programa siguen siendo ejecutables
externos. La shell portátil nunca afirma que esos programas están disponibles.

### `code_search`

`code_search` conserva `rg` como acelerador cuando ya existe en el toolchain o
en `PATH`. Si no existe, usa `internal/textsearch`, un motor escrito en Go que:

- acepta regex RE2 o búsqueda literal;
- admite `ignore_case`, `glob`, `path`, contexto y límite;
- entiende globs recursivos como `internal/**/*.go`;
- entrega el mismo formato estable `path:line:text`;
- ordena archivos para resultados deterministas;
- respeta `.gitignore`, `.lilithignore` y los demás archivos de exclusión que
  ya reconoce Lilith;
- omite por defecto archivos ocultos, metadatos VCS, dependencias, builds,
  binarios, symlinks recursivos y archivos mayores de 16 MiB;
- limita cada línea a 500 runas y respeta cancelación.

La ausencia de ripgrep deja de ser un error y no justifica instalarlo sólo para
leer el código.

## Prompts y descubrimiento de herramientas

- El mensaje de sistema explica que `code_search` no depende de ripgrep.
- El perfil de entorno comunica la shell nativa y la existencia del respaldo
  portátil sin presentarlo como una distribución Linux.
- El schema de `run_terminal_command` incorpora `portable` y enumera su toolbox
  y sus límites.
- `tool_search` reconoce búsquedas por `rg`/`ripgrep`, `portable`, `POSIX` y
  expresiones como “sin Bash”.
- Los hints prohíben asumir Git, Go, npm, Docker o Make sólo porque la shell
  portátil esté activa.

## Instalación y distribución

- `mvdan.cc/sh/v3 v3.13.1` se enlaza como código Go dentro del mismo binario.
- No se extrae un ejecutable Bash ni se añade CGO.
- El catálogo histórico de ripgrep/BusyBox continúa disponible como acelerador
  opcional y compatibilidad para workflows antiguos.
- Termux ya no instala ripgrep como dependencia; sólo Git y Go son necesarios
  para clonar y compilar el proyecto desde fuente.
- La licencia BSD-3-Clause se conserva en `THIRD_PARTY_NOTICES.md`.

## Archivos principales

- `internal/shell/portable.go`
- `internal/shell/resolver.go`
- `internal/textsearch/search.go`
- `internal/tools/exec.go`
- `internal/tools/registry.go`
- `internal/tui/chat.go`
- `internal/codeintel/manager.go`
- `install.sh`
- `install.md`
- `THIRD_PARTY_NOTICES.md`

## Pruebas

- resolución explícita y automática de `portable`;
- prioridad de shells y comandos nativos;
- sintaxis Bash, pipes y redirecciones;
- toolbox de búsqueda y archivos;
- error 127 claro para ejecutables ausentes;
- cancelación y timeout;
- búsqueda Go literal/regex, glob, contexto, límites, binarios y directorios
  pesados;
- schemas, prompts, selección perezosa y `tool_search`;
- instalador Termux sin dependencia de ripgrep.

## Pruebas manuales recomendadas

```powershell
go mod tidy
test.cmd
go run .\cmd\li\main.go
```

Dentro de Lilith:

```text
run_terminal_command shell=portable command="name=Lilith; printf '%s\n' \"$name\" | grep Lilith"
code_search pattern="ShellPortable" literal=true glob="*.go"
```

En un entorno sin `rg`, `code_search` debe conservar el mismo contrato. En un
entorno con `rg`, debe seguir utilizándolo como acelerador.

## Validación realizada en el entorno de entrega

- `gofmt` y `git diff --check` sin errores;
- análisis sintáctico de 293 archivos Go;
- pruebas y `go vet` de `internal/textsearch` en un módulo aislado;
- comprobación de tipos de `internal/shell` contra las firmas públicas de
  `mvdan.cc/sh/v3/interp`, `expand` y `syntax`;
- simulaciones del instalador Linux/Termux y validación `sh -n`;
- revisión de que no se generen ejecutables auxiliares ni se añada CGO.

La suite integral del repositorio debe ejecutarse en Windows/Linux con Go
1.25.12. El entorno de construcción usado para esta entrega sólo dispone de Go
1.23.2 y no puede descargar toolchains, por lo que no se declara una ejecución
integral que no ocurrió.
