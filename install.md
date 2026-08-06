# Instalación y referencia técnica de Lilith

Los instaladores se descargan directamente desde la rama `main`. Esto permite
corregir `install.sh`, `install.ps1` o `install.cmd` sin volver a compilar ni
publicar los binarios. La configuración, sesiones y credenciales permanecen en
`~/.li` y no se eliminan durante una actualización.

## Linux

Arquitecturas: AMD64, ARM64 y ARMv7.

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | bash
```

El instalador descarga el binario del último release, verifica
`SHA256SUMS.txt` y lo coloca en un directorio que ya pertenece al `PATH`,
normalmente `/usr/local/bin`. No modifica `.bashrc` ni requiere ejecutar
`source ~/.bashrc`.

Versión concreta:

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh -o install.sh
sh install.sh 0.3.0
rm install.sh
```

También se puede usar `LI_VERSION=v0.3.0` o `LI_REPOSITORY` para un fork.

## Termux en Android

Compatibilidad oficial inicial: ARM64/AArch64.

```bash
pkg install -y curl
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | sh
```

En Termux no se descarga un binario Android del release. El instalador:

1. instala o actualiza `git`, `golang` y `ripgrep` mediante `pkg`;
2. clona únicamente el último commit de la rama predeterminada del repositorio;
3. no descarga tags ni el historial completo;
4. compila `./cmd/li` nativamente con Go de Termux;
5. reemplaza de forma segura `$PREFIX/bin/li`.

El clon superficial se conserva en `$HOME/.local/share/lilith/source` para
facilitar diagnóstico y actualizaciones. Puede cambiarse su destino con
`LI_TERMUX_SOURCE_DIR`. En Termux no se fija `LI_VERSION`: cada ejecución
compila el código más reciente publicado en la rama predeterminada.

El runtime corrige además el argumento ejecutable duplicado que Android puede
introducir al lanzar programas Go; sin esa normalización Cobra interpretaría la
ruta absoluta de `li` como un comando desconocido.

## Windows PowerShell

Arquitecturas: AMD64 y ARM64.

```powershell
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

Se instala en `%LOCALAPPDATA%\Programs\Lilith\bin\li.exe`. El script detecta la
arquitectura tanto en PowerShell 7 como en Windows PowerShell 5.1, agrega el
directorio al `PATH` persistente del usuario y también a la sesión actual.

Versión concreta:

```powershell
$env:LI_VERSION = '0.3.0'
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

## Windows CMD

Descarga el archivo directamente desde el repositorio y ejecútalo:

```cmd
curl.exe -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.cmd -o install.cmd
install.cmd
```

Versión concreta:

```cmd
install.cmd 0.3.0
```

## Primer arranque

Al ejecutar `li` por primera vez aparece un onboarding con estas opciones:

1. proveedor personalizado OpenAI-compatible;
2. ChatGPT Codex mediante OAuth;
3. continuar con los modelos gratuitos de OpenCode Free.

Después puede volver a esa pantalla con `/login`.

## Actualizar

Vuelve a ejecutar el mismo comando de instalación. Linux y Windows reemplazan
el ejecutable usando el release solicitado; Termux vuelve a clonar únicamente
el último commit de la rama predeterminada y lo compila nativamente.

```bash
li version
```

## Compilar manualmente

Requiere Go 1.25.12 o superior:

```bash
git clone https://github.com/YahirHub/Lilith-Code.git lilith
cd lilith
go run ./cmd/build build
```

El builder aplica automáticamente `CGO_ENABLED=0` y `-tags=grammar_set_core`
para incorporar las gramáticas de inteligencia de código dentro de cada
binario. Un build directo equivalente es:

```bash
CGO_ENABLED=0 go build -tags=grammar_set_core -trimpath -o li ./cmd/li
```

`cmd/build` genera los binarios de release para Linux y Windows. Termux se
compila en el propio dispositivo mediante `install.sh`.

## Pruebas locales

Desde CMD o PowerShell en Windows:

```cmd
test.cmd
```

Para ejecutar también `go vet`:

```cmd
test.cmd -Vet
```

El helper ejecuta estas validaciones:

```bash
go mod tidy -diff
go test -mod=readonly -tags=grammar_set_core -count=1 -timeout=15m ./...
go mod verify
```

No debe ejecutarse `go mod download all`: esa orden recorre módulos que no
participan en la compilación y puede fallar por dependencias de herramientas o
tests pertenecientes a terceros.

Si Windows no puede resolver `sum.golang.org`, `test.cmd` prueba temporalmente
`sum.golang.google.cn`, alias reconocido por Go para la misma base de checksums.
Nunca desactiva `GOSUMDB` ni cambia la configuración global de Go.

La regresión de cancelación anidada puede repetirse de manera aislada:

```cmd
go test -mod=readonly -tags=grammar_set_core -count=10 -run "^TestCancelParentTearsDownNestedAgentTree$" ./internal/subagents
```

## SSH, perfiles y bóveda de credenciales

La herramienta interna `ssh_remote` mantiene conexiones durante la ejecución de
Lilith y expone acciones equivalentes a Codewolf para:

- registrar, consultar, renombrar, actualizar y eliminar servidores;
- conectar mediante contraseña, clave privada, `SSH_AUTH_SOCK` o variables de entorno;
- fijar opcionalmente la huella SHA-256 de la clave del host;
- ejecutar comandos, conservar el directorio remoto y abrir shells PTY persistentes;
- listar, leer, escribir, subir, descargar, renombrar y eliminar mediante SFTP;
- consultar, desbloquear, bloquear y cambiar la contraseña maestra de la bóveda.

Los perfiles sin secretos se guardan en:

```text
~/.li/ssh-servers.json
```

Las contraseñas y passphrases que el usuario decide conservar se guardan en:

```text
~/.li/ssh-secrets.enc
```

La bóveda deriva una clave de 256 bits con scrypt (`N=32768`, `r=8`, `p=1`) y
cifra el contenido mediante AES-256-GCM con salt e IV aleatorios. La contraseña
maestra nunca se persiste. La clave derivada y el contenido descifrado sólo
permanecen en memoria mientras el proceso está abierto; `li` cierra conexiones,
shells, agentes SSH y bloquea la bóveda al terminar.

Los prompts de contraseña se transportan por un puente local entre la herramienta
y la TUI. El valor enmascarado no forma parte de los argumentos del tool call, del
historial, de la sesión ni de los logs. `/config > Seguridad` permite controlar el
modo seguro, que exige una confirmación local antes de operaciones SSH sensibles.

`private_key_path` relativo se resuelve desde la raíz del proyecto. En Linux,
macOS y Termux se admite el socket Unix de `SSH_AUTH_SOCK`. En Windows se admiten
rutas AF_UNIX expuestas por `SSH_AUTH_SOCK` y named pipes; cuando no existe una
variable configurada, Lilith intenta de forma segura el agente OpenSSH integrado
en `\\.\pipe\openssh-ssh-agent` sin convertir su ausencia en un error de conexión.

Para rutas remotas protegidas, `ssh_remote` usa `privilege_mode=auto` por defecto. Primero prueba SFTP; ante `permission denied`, prepara el archivo bajo `/tmp` o una ruta escribible y lo instala con UID 0, `sudo` o `doas -n`. La contraseña sudo usa `interaction.SecretSudoPassword`, se envía únicamente por stdin con `sudo -S -p ''` y se mantiene en memoria por conexión; nunca se serializa. `never` desactiva el fallback y `required` eleva desde el inicio. Las operaciones de lectura, descarga, escritura, subida, creación, renombrado, borrado, stat y listado comparten esta capa. Un `exec` arbitrario sólo se eleva cuando se solicita `required`, porque reintentarlo tras un fallo podría duplicar efectos parciales.

## GitZip local y remoto

La herramienta interna `gitzip` admite `create`, `upload`, `remote_create` y
`remote_extract`, con formatos `zip`, `tar` y `tar.gz`.

Para crear el manifiesto recorre reglas anidadas de:

```text
.gitignore
.lilithignore
.codewolfignore
.codebuffignore
.manicodeignore
```

Siempre excluye cualquier directorio `.git`, el archivo de salida y los
manifiestos temporales. También excluye `.env`, `.env.local` y variantes reales;
`.env.example`, `.env.sample`, `.env.template`, `.env.dist` y `.env.defaults` sí
pueden incluirse. Cuando `include_protected_env=true`, Lilith pide una segunda
confirmación local si la protección está activa.

Las operaciones remotas reutilizan un `connection_id` abierto por `ssh_remote`.
La creación TAR remota usa un manifiesto NUL y `tar --no-recursion --null -T`; los
argumentos adicionales se limitan a una allowlist conservadora. ZIP usa una lista
explícita de archivos. Ninguna operación remota abre una segunda conexión
implícita ni almacena credenciales dentro del archivo. Antes de crear o extraer remotamente, GitZip comprueba acceso de lectura al origen y de escritura al destino; si necesita privilegios, ejecuta el comando completo elevado desde el principio y conserva el mismo `connection_id`.

Dependencias nuevas del runtime:

- `golang.org/x/crypto` para SSH, agentes y scrypt;
- `github.com/pkg/sftp` para transferencias y operaciones de archivos remotos.

Estas versiones requieren Go 1.25.12, que pasa a ser la versión mínima del proyecto.

## Skills y carga de instrucciones

Lilith usa un único loader para skills embebidas, globales y específicas del
proyecto. La precedencia es:

```text
proyecto > usuario > embebida
```

`SkillsEnabled` controla toda la infraestructura y `DisabledSkills` conserva las
excepciones individuales por nombre. Una skill desactivada no se ofrece en la
activación automática, la paleta, los agentes ni la invocación manual.

La skill embebida `ponytail-development` se distribuye mediante `go:embed` y su
contenido completo sólo se carga cuando la tarea coincide o se invoca
explícitamente.

## Orquestador y subagentes

Las invocaciones `Agent`, `Task`, `task` y `agent` convergen en el mismo runtime.
Un lote compuesto sólo por agentes puede ejecutarse en paralelo, pero los
resultados regresan al proveedor en el orden original.

La cancelación se hereda por todo el árbol. `/clear`, el cambio de sesión y la
salida cancelan el trabajo background de la conversación anterior. Los eventos
incluyen una generación de sesión para impedir que una finalización atrasada se
muestre en otro chat.

Las tareas background producen un evento terminal incluso si fallan antes de
crear la sesión hija. Sus finalizaciones se persisten y se entregan una sola vez
al agente padre antes de la siguiente solicitud del usuario.

La reanudación por `task_id` utiliza el provider, modelo y worktree guardados. Si
alguno ya no existe, la reanudación falla explícitamente en lugar de cambiar al
modelo actual o trabajar sobre el checkout principal.

## Inteligencia de código

`internal/codeintel` es independiente de la TUI y se comparte entre el agente
principal y los subagentes cuando trabajan sobre la misma raíz.

El motor mantiene un índice incremental fuera del repositorio, bajo
`~/.li/codeintel/`, utiliza Tree-sitter en Go puro con el conjunto Core100
embebido y agrega una capa `go/ast` para proyectos Go. LSP y SCIP son mejoras
opcionales: sólo se usan cuando sus ejecutables o índices ya existen.

Las herramientas disponibles incluyen estado, símbolos, referencias, grafo,
contexto, búsqueda semántica, SCIP, validación y formato validado.

## Escritura segura de archivos

`write_file` y `append_file` reciben contenido estructurado y no lo convierten en
heredocs, `printf`, here-strings ni comandos de shell.

Las escrituras completas se crean en un temporal del mismo directorio, se
sincronizan, verifican y publican mediante reemplazo atómico. Una cancelación no
puede dejar el destino truncado.

Límites actuales:

- `write_file`: hasta 1 MiB por llamada;
- `append_file`: hasta 1 MiB por sección y 64 MiB para el archivo final;
- reemplazar un archivo existente requiere `overwrite=true`;
- `expected_sha256` permite detectar cambios ocurridos desde la lectura previa;
- `str_replace` exige `path` más un par completo `old`/`new`, o un `edits[]` no vacío;
- los alias compatibles `old_string`/`new_string`, `oldText`/`newText` y variantes equivalentes se normalizan sin perder la vista previa;
- omitir `new` ya no se interpreta como borrado: una eliminación requiere enviarlo explícitamente como cadena vacía.

## Ejecución de comandos y shells

`run_terminal_command` acepta `shell=auto|powershell|cmd|bash|sh`.

En Windows, el modo automático usa PowerShell para comandos neutrales, CMD para
sintaxis CMD y Bash/sh para sintaxis POSIX cuando está disponible. En Linux,
macOS y Termux se prefiere Bash y se usa `sh` como respaldo.

PowerShell se configura como UTF-8 sin BOM antes de ejecutar el comando para
conservar acentos y emojis. La sentencia solicitada permanece al final para no
alterar el código de salida.

La herramienta rechaza heredocs incompletos y escrituras inline demasiado
largas antes de iniciar el proceso.

Para búsquedas dentro del repositorio debe preferirse `code_search`. Cuando el
agente usa un `grep -r` o `grep -R` simple sin una ruta explícita, Lilith lo
rechaza antes de crear el proceso y le indica que use `code_search` o una ruta
concreta. Las búsquedas mediante `grep` recursivo, `rg`, `find` y `git grep`
reciben un límite seguro de 30 segundos si no se indicó `timeout_seconds`;
compilaciones, tests e instalaciones continúan sin timeout implícito.

## Releases

La versión se define en `internal/version/version.go`. Para publicar una nueva
versión:

1. cambia `version.Current`;
2. crea el commit correspondiente;
3. ejecuta manualmente **Publicar release** desde GitHub Actions.

El workflow usa un único runner `ubuntu-latest` para reducir consumo. Primero
valida `go mod tidy -diff`, ejecuta las pruebas con `-mod=readonly`, aplica
`go vet` y `go mod verify`, y después publica desde el mismo job.

Como Lilith se construye con `CGO_ENABLED=0`, el builder puede generar desde
Linux los binarios `linux/amd64`, `linux/arm64`, `linux/armv7`,
`windows/amd64` y `windows/arm64` cambiando `GOOS` y `GOARCH`; no necesita
MinGW ni un runner Windows.

Antes del release también se compilan —sin ejecutarlos— los binarios de test
Windows de `internal/tools`, `internal/shell`, `internal/subagents` e
`internal/tui`. Esto detecta errores de compilación específicos del sistema.
La ejecución real de Windows PowerShell 5.1, CMD y del runtime `.exe` no puede
reproducirse en Ubuntu, por lo que los cambios dependientes de esas rutas deben
validarse además localmente con `test.cmd` en Windows.
