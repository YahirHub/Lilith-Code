# Contexto maestro de Lilith Code

> Estado consolidado para retomar el proyecto sin depender del historial del chat. Antes de trabajar, leer también `AGENTS.md`, el documento numerado más reciente de `contexto/`, `git status` y los últimos commits.

## 1. Producto

Lilith (`li`) es un agente de programación interactivo para terminal, implementado en Go. Incluye chat con streaming, tool calls, edición de archivos, shell, skills, subagentes, MCP, plugins compatibles con Claude, historial persistente, modos Build/Plan/Goal, tareas, goals durables, búsqueda web y OCR estructural local.

El proyecto conserva un diseño inspirado en agentes de terminal modernos, pero su implementación actual es propia.

## 2. Stack vigente

- Go 1.25.12+.
- `tview v0.42.0` como runtime interactivo.
- Tcell como backend de pantalla, teclado, ratón y pegado.
- Widgets y ciclo lógico propios en `internal/tui/uikit`.
- `rivo/uniseg` para ancho Unicode.
- Cobra para la CLI.
- Binario objetivo con `CGO_ENABLED=0`.
- Termux ARM64 se instala desde `scripts/install.sh`: usa `pkg`, clona sólo el último commit de la rama predeterminada con `--depth 1 --single-branch --no-tags` y compila `cmd/li` nativamente; no se fija tag ni se publica un asset Android no verificado.

No quedan dependencias de Bubble Tea, Bubbles, Lip Gloss, Glamour ni otros módulos Charmbracelet. No deben reintroducirse.

## 3. Estructura principal

```text
cmd/li/                       entrada CLI
internal/config/              ajustes persistentes
internal/secrets/             API keys y OAuth
internal/providers/           proveedores, conexión y catálogos
internal/providers/openai/    chat completions, Responses/Codex, reasoning y transporte resiliente
internal/compaction/           auto compactación y resumen iterativo del contexto
internal/rewind/               checkpoints, restauración de código y forks de workspace
internal/tui/                 chat y pantallas interactivas
internal/tui/uikit/           componentes TUI propios
internal/version/             versión SemVer única para binarios y releases
internal/tools/               herramientas del agente
internal/sshremote/           perfiles, bóveda, conexiones SSH, shells y SFTP
internal/gitzip/              manifiestos ignore y archivos ZIP/TAR/TAR.GZ
internal/interaction/         prompts locales de secretos y confirmaciones
internal/skills/              runtime y recursos de Agent Skills
assets/skills/                skills genéricas embebidas en el binario
internal/codeintel/           índice sintáctico, LSP, SCIP y validación por lenguaje
internal/plan/                estado y políticas Plan
internal/goal/                objetivos persistentes
internal/todo/                TodoWrite persistente
internal/subagents/           ejecución de subagentes
internal/imageocr/            OCR nativo Windows y modelo estructural
scripts/                      instaladores y helpers de pruebas multiplataforma
contexto/                     decisiones técnicas numeradas
AGENTS.md                     instrucciones resumidas para Codex/agentes
```

## 4. Runtime TUI

`tview.Application` es el único propietario de la terminal. La aplicación conserva su apariencia mediante un `tview.TextView` que recibe el frame ANSI generado por los componentes internos. Tcell entrega teclado, mouse y pegado.

Reglas críticas:

- el chat sigue funcionando aunque `/config`, `/models` u otra pantalla esté abierta;
- los mensajes de streaming se enrutan siempre al `ChatModel` persistente;
- el ratón se captura sólo cuando hay controles clicables, para mantener selección de texto nativa;
- el pegado se entrega como bloque atómico;
- la última columna se reserva para la scrollbar;
- `Style.Width` es ancho de contenido: bordes y padding se suman aparte;
- el input tiene límite de caracteres independiente de sus ocho filas visibles;
- el loop que consume mensajes, SSE y timers no espera el dibujo físico de `tview`; publica el frame más reciente en una cola independiente con cadencia limitada;
- el transcript conserva el historial estable como segmentos de líneas y sólo vuelve a procesar la cola mutable, evitando trabajo proporcional a toda la conversación por cada token;
- Return recibido como `Ctrl+M` por ciertos PTY/SSH se normaliza a Enter, por lo que enviar no depende de cómo el terminal codifique CR;
- una petición de proveedor nunca bloquea el loop de teclado: conexión, streaming, reintentos y watchdog corren fuera del estado TUI.
- `Ctrl+C` vacía el borrador visible y cierra su paleta sin cancelar el turno activo ni tocar la cola; `Esc` sigue siendo la interrupción de la tarea.

## 5. Proveedores, autenticación y catálogos

Persistencia bajo el directorio de configuración:

| Archivo | Contenido |
|---|---|
| `providers.json` | proveedores personalizados, selección activa y catálogos custom |
| `provider-auth.json` | API keys y tokens OAuth |
| `provider-model-cache.json` | última respuesta válida de catálogos bundled |

Tipos de autenticación:

- `bundled` y `none`: disponibles sin secreto;
- `api_key`: requiere clave guardada;
- `env`: requiere variable de entorno;
- `oauth`: requiere sesión OAuth.

`/providers` muestra todas las conexiones para poder configurarlas. `/models` muestra exclusivamente modelos de proveedores conectados. Un proveedor desconectado no puede quedar activo ni ser seleccionado.

Al abrir `/models`, Lilith consulta en segundo plano el catálogo de cada proveedor conectado. `Ctrl+R` repite la consulta sin bloquear la escritura de la letra `r` en el filtro. Los endpoints OpenAI-compatible usan `GET {baseURL}/models`; Codex usa su catálogo autenticado de cuenta. Los proveedores se actualizan en paralelo y un fallo conserva la caché anterior sin impedir los demás.
Si el endpoint de catálogo responde 404, 405 o 501, el proveedor se considera compatible sólo con catálogo manual: no se presenta un error, no se eliminan modelos configurados y futuras aperturas de `/models` pueden volver a intentar el descubrimiento. Los fallos reales de red, autenticación o respuestas inválidas sí se reportan de forma no bloqueante.

El alta de proveedores personalizados usa campos de una línea con viewport horizontal ligado al cursor. La URL base y la API key aceptan hasta 16,384 runes y el render usa el ancho real de la caja; pegar un endpoint largo nunca debe recortar el valor aunque sólo una sección sea visible en terminales estrechas.

El proveedor OAuth integrado se muestra como **ChatGPT Codex** en onboarding, login, selectores y estado activo; Plus/Pro describe la suscripción requerida, no el nombre visible.

Los modelos nuevos de proveedores custom se guardan en `providers.json`. Los de proveedores bundled se guardan en `provider-model-cache.json`, por lo que permanecen disponibles tras cambiar de pantalla, reiniciar o perder temporalmente la conexión.

## 6. Modos Build, Plan y Goal

`Tab` y `Shift+Tab` alternan Goal ↔ Build; Plan se selecciona explícitamente con `/plan` y Tab vuelve de Plan a Build. El modo elegido aplica al próximo mensaje, mientras un turno en ejecución conserva su snapshot.

- **Build:** implementación normal y herramientas mutantes.
- **Plan:** sólo lectura; puede investigar, preguntar decisiones y entregar un plan. El cambio Plan → Build puede consumir una vez el plan aprobado.
- **Goal:** el texto introducido sustituye el objetivo persistente, vuelve automáticamente al selector Build y arranca o reorienta una ejecución autónoma con herramientas Build.
  Goal no aplica límites artificiales de tokens, pasos, turnos o tiempo. Los contadores de tokens/tiempo son sólo diagnósticos; los estados antiguos por presupuesto/cuota se reactivan al cargar. `goal_complete` completa con resumen explícito; `/resume` reabre el mismo objetivo completado/pausado/bloqueado/interrumpido. Cerrar Lilith, recuperar una sesión que seguía activa o sufrir un fallo definitivo deja el goal `interrupted` y muestra Continuar. Mientras existe un goal activo, pausado, bloqueado o interrumpido, `create_goal` deja de exponerse al modelo. Repetir exactamente el mismo objetivo activo es idempotente y no reinicia tiempos ni contadores.

Los estados se persisten en la sesión. Goal comparte las capacidades de implementación de Build; Plan conserva su política restrictiva.

`/init [instrucciones adicionales]` siempre ejecuta la inicialización normal y añade esas indicaciones en un bloque de una sola ejecución. No crea Goal ni conserva las indicaciones como estado separado.

Knowledge vive separado de Skills en `assets/knowledge/<namespace>/**`, se indexa sólo con la primera consulta y se expone mediante `knowledge_search`, `knowledge_read` y `knowledge_topics`. El prompt exige consultarlo antes de adivinar sintaxis incierta de plataforma/herramienta. La build pública incorpora el namespace `public`; distribuciones estáticas downstream pueden registrar `company` desde módulos privados. Un dominio con Skill y referencias propias conserva allí una única fuente de verdad: Git/GitHub y Docker/Compose no se replican en Knowledge. Las referencias públicas actuales cubren Windows, Linux, Termux, ADB y arquitectura/desarrollo de Lilith.

## 7. Chat y ejecución

- Streaming SSE/Responses con normalización por proveedor.
- Reasoning separado del mensaje final, incluidos campos estructurados y etiquetas inline como `<think>`.
- Tool calls con paneles en vivo y persistentes.
- Las rutas de herramientas de archivos se validan centralmente; valores placeholder como `null`, `undefined`, `nil`, `<nil>` o `(null)` se rechazan y nunca se convierten en archivos físicos.
- El shell normaliza redirecciones accidentales como `> null` o `2> null` a `/dev/null`, porque Lilith ejecuta un shell POSIX también en Windows.
- En Unix/Android, shell y hooks resuelven `bash`/`sh` mediante `PATH`; no se hardcodea `/bin/sh`, inexistente en Termux.
- `run_terminal_command` no impone límite de ejecución cuando `timeout_seconds` no está presente. Los builds, instalaciones y pruebas largas siguen ejecutándose hasta completar o hasta una cancelación explícita; un timeout positivo conserva el corte y la limpieza del árbol de procesos.
- Cola de steering y follow-up sin abrir turnos paralelos. Si el proveedor falla, el siguiente mensaje en cola se consume en esa frontera de error y no queda varado como si Enter se hubiera ignorado.
- El cliente de proveedor no usa un timeout HTTP total: limita dial, TLS y espera de headers, usa TCP keepalive y corta sólo un stream que permanezca sin bytes durante cuatro minutos.
- Los cortes de transporte no terminan el turno ni muestran errores crudos como `dial tcp`: Lilith comprueba primero el endpoint activo y luego conectividad pública, distingue Internet caído de proveedor inaccesible, espera con backoff cancelable por `Esc` y reintenta automáticamente al recuperarse.
- Si el corte ocurrió después de recibir texto, reasoning o una tool call parcial, se elimina únicamente ese intento incompleto de la TUI y se repite el request original. El prompt, historial estable, cola y turno permanecen intactos.
- Cancelación con Esc; `/exit` es la salida explícita.
- TodoWrite, planes y goals se guardan en la sesión.
- Skills y agentes pueden usar modelo heredado, explícito o lista de preferencias.
- El orquestador ejecuta en paralelo los lotes puros de `Agent` preservando el orden protocolario de resultados. La cancelación se propaga a hijos anidados y `/clear`/cambio de sesión separan generaciones de eventos para impedir contaminación entre conversaciones.
- La pantalla persistente de chat se conserva siempre como `*ChatModel`. El modelo contiene `atomic.Uint64` para aislar generaciones de agentes y nunca debe copiarse por valor; `NewChat` devuelve directamente el puntero usado por el router y las pantallas auxiliares.
- Las tareas background siempre terminan con evento `completed`, `failed` o `canceled`; su panel se persiste y la notificación se entrega exactamente una vez antes del siguiente prompt actual. Desactivar background fuerza semántica foreground completa.
- `task_id` reanuda con provider/modelo persistidos, rechaza reanudaciones concurrentes y falla de forma cerrada si el worktree aislado desapareció. Las sesiones hijas se publican mediante temporales únicos y reemplazo después de `Sync`.
- El binario incluye `ponytail-development`, una metodología universal de desarrollo conservada como Agent Skill. `/config > Skills` mantiene un interruptor maestro y excepciones individuales persistidas en `disabledSkills`; una skill desactivada no aparece en activación automática, paleta, agentes ni invocación manual.
- MCP y plugins siguen ejecutándose aunque una pantalla auxiliar esté abierta.

## 8. Compactación automática de contexto

Lilith compacta el contexto activo antes de agotar la ventana del modelo:

- umbral predeterminado: `contextWindow - 16,384` tokens, contando mensajes y schemas de herramientas; para ventanas menores a la reserva se usa un fallback proporcional;
- conserva una cola exacta de hasta 20,000 tokens, adaptada a `contextWindow/4` en modelos pequeños, y mantiene dos turnos recientes completos cuando caben; si el system prompt o los schemas disparan el umbral aunque todo el historial quepa en esa cola, resume los turnos anteriores y conserva exacta la solicitud más reciente;
- selecciona el corte con la misma poda de tool outputs que usa el request real, pero resume y archiva los mensajes originales exactos; un turno único enorme puede cortarse en una frontera segura de assistant, nunca en un resultado de herramienta;
- reutiliza el resumen previo como contexto iterativo en compactaciones posteriores y limita el tamaño total de la solicitud de resumen;
- reconstruye el historial enviado al proveedor como `resumen + cola exacta`;
- si el proveedor devuelve un error reconocible de overflow, compacta y reintenta el turno;
- `/compact [instrucciones opcionales]` fuerza la operación manualmente cuando no hay un turno activo; las instrucciones opcionales enfocan el handoff y el turno más reciente permanece exacto.

La compactación no elimina la experiencia visible ni los datos originales. El transcript permanece completo y cada prefijo retirado del contexto se guarda en `Session.Compactions` con resumen, tokens aproximados y mensajes archivados. `/history` cuenta también esos turnos archivados. Esc puede cancelar una compactación manual; una compactación automática pertenece al contexto cancelable del turno.

## 9. Rewind y forks de sesión

Lilith mantiene puntos de restauración por proyecto y sesión bajo el directorio de configuración:

- al iniciar una nueva acción del usuario guarda el estado exacto de la conversación, transcript, Todo, Plan, Goal y compactaciones;
- el snapshot de código se toma de forma perezosa justo antes de la primera herramienta, hook o subagente potencialmente mutante, evitando escanear el proyecto en turnos de sólo lectura;
- `/rewind` abre un selector de mensajes y permite restaurar código + conversación, sólo conversación o sólo código; se bloquea mientras exista un turno, comando directo o subagente background activo;
- al restaurar la conversación se mantiene el ID de la sesión activa, se recorta al checkpoint y el mensaje seleccionado vuelve al editor;
- antes de efectuar el rewind se crea un punto de seguridad del estado actual, de modo que la propia restauración pueda revertirse desde `/rewind`; en modo sólo conversación ese punto no captura el workspace porque ningún archivo va a cambiar;
- las operaciones de código de `/rewind` son cancelables con `Esc`/`Q`, tienen timeout y descartan resultados tardíos. Los procesos Git se ejecutan sin prompts interactivos ocultos; una cancelación durante una restauración de archivos puede dejar una aplicación parcial y la UI debe advertir que se revise el workspace;
- se conservan como máximo 80 puntos por sesión. Los puntos anteriores a la introducción de esta función no pueden reconstruirse retroactivamente.

En Git, el snapshot usa un índice temporal separado del índice real, crea un commit interno y lo fija bajo `refs/lilith/rewind/<sesión>/<punto>`. El staging del usuario no se altera. Al restaurar se materializa únicamente el path del proyecto activo y se eliminan dentro de ese scope los archivos tracked o untracked no ignorados que no existían en el punto. En monorepos, directorios hermanos quedan intactos. Los archivos ignorados generados quedan fuera salvo que ya estuvieran tracked.
Las operaciones que introducen o extraen contenido del índice temporal desactivan explícitamente el `core.autocrlf` global. Así un checkpoint capturado con LF no reaparece como CRLF sólo por ejecutarse en Windows, y un fork materializa los mismos bytes del snapshot. Los atributos propios del repositorio siguen siendo autoritativos.

Fuera de Git, se usa un manifiesto con blobs SHA-256. Se excluyen `.git`, `.lilith`, `.cache`, `node_modules`, `.next`, `dist`, `build` y `target`; cada archivo está limitado a 32 MiB y el snapshot a 512 MiB. Un punto parcial sigue permitiendo restaurar lo capturado, pero la UI debe advertirlo.

`/fork [título opcional]` abre un navegador de carpetas dentro de la propia TUI antes de capturar el estado. Empieza junto a la raíz del workspace fuente —la raíz del repositorio en Git o el proyecto activo fuera de Git—, permite abrir carpetas, volver al directorio padre, recorrer unidades en Windows y crear una carpeta nueva. Todas las acciones tienen atajos de teclado para funcionar por SSH; cuando Tcell recibe eventos de mouse también admite clic y rueda. El usuario debe elegir una carpeta existente y vacía fuera del workspace original.

Después de elegir el destino, `/fork` captura el estado actual y crea una sesión independiente con nuevo ID y `ForkedFrom`. Se rechaza mientras haya un turno, comando directo o subagente background activo. Para Git materializa un worktree separado en el commit del snapshot; en fallback reconstruye una copia independiente desde los blobs. Lilith cambia al nuevo directorio y reconecta MCP. La sesión original, sus archivos y su historial de rewind permanecen intactos; el fork no hereda checkpoints antiguos.

## 10. OCR estructural

`extract_image_text` permite a modelos sin visión procesar capturas y documentos sin subir la imagen:

- Windows: `Windows.Media.Ocr` mediante WinRT en Go puro.
- Otros sistemas/fallback: Tesseract externo opcional.
- Salidas: texto, layout monoespaciado, regiones, separadores, coordenadas y JSON.
- Mantiene `CGO_ENABLED=0` porque no enlaza una biblioteca OCR al binario.


## 10 bis. Inteligencia de código

Lilith incluye un motor independiente en `internal/codeintel`:

- detecta host, distribución, WSL, SSH, contenedor, Termux, shell, arquitectura, manifests, frameworks, lenguajes, package manager y herramientas disponibles;
- usa Tree-sitter en Go puro con gramáticas Core100 embebidas mediante el tag `grammar_set_core`, sin CGO ni archivos externos;
- mantiene un índice incremental y transaccional bajo `~/.li/codeintel/`, nunca dentro del workspace, y expone su ruta física en el estado;
- combina Tree-sitter con `go/ast` para indexar constantes/variables Go, nombres canónicos y referencias por alias de importación;
- expone símbolos, referencias precisas cuando existe identidad calificada, un grafo conectado y contexto por declaraciones con ranking bilingüe orientado a código fuente;
- puede consultar LSP e `index.scip` únicamente cuando ya existen en el sistema; `gopls` nunca se incrusta ni se descarga y Go usa un fallback semántico estático interno cuando falta;
- selecciona formatters, compiladores y tests mediante adaptadores para Go, Rust, Node/TypeScript, Deno, Python, PHP/Laravel, Ruby, Dart/Flutter, Swift, Elixir, .NET, Godot, Maven, Gradle, CMake y Make, sin instalar dependencias; en Windows descarta Makefiles POSIX secundarios;
- rechaza rutas de validación que escapen de la raíz y no aplica formato global implícito.

El perfil ligero se agrega únicamente al mensaje de sistema del agente principal y los subagentes; nunca crea ni modifica un turno de usuario y no provoca el escaneo completo. El índice sólo se refresca al invocar herramientas de inteligencia de código.

## 10 ter. Escritura nativa y atómica

Las escrituras extensas del agente ya no dependen de heredocs o literales largos
en `run_terminal_command`:

- `write_file` recibe el contenido completo, exige `overwrite=true` para un destino existente, admite `expected_sha256` y limita cada llamada a 1 MiB;
- `append_file` construye documentos largos por secciones de hasta 1 MiB, no inserta saltos de línea automáticamente y limita el archivo final a 64 MiB;
- `create_file`, `str_replace` y `apply_diff` usan el mismo backend atómico;
- el backend escribe un temporal en el directorio del destino, preserva permisos, sincroniza, publica sin ventana de contenido parcial y verifica bytes/SHA-256;
- la creación estricta usa publicación no-clobber para no sobrescribir un archivo que aparezca después del preflight;
- la shell rechaza heredocs sin delimitador y escrituras inline superiores al umbral seguro antes de iniciar el proceso;
- un mismatch de `str_replace` devuelve el estado actual, SHA-256 y una región cercana para que el modelo vuelva a leer y reintente con texto vigente.

La TUI renderiza `write_file` y `append_file` como paneles de archivo. Cuando un
modelo intenta reemplazar sin autorización, se interrumpe el stream de argumentos,
se compacta el cuerpo rechazado y se devuelve `OVERWRITE_REQUIRED` sin tocar disco.

## 10 quater. Shell nativa por sistema

`run_terminal_command` selecciona el intérprete según host y sintaxis:

- Windows usa PowerShell para comandos neutrales, CMD para sintaxis CMD y Bash/sh únicamente para sintaxis POSIX cuando está disponible; reconoce asignaciones `VAR=value` seguidas por comando, `;`, salto de línea o fin de entrada;
- Linux, macOS y Termux usan Bash con fallback a `sh`;
- el parámetro `shell=auto|powershell|cmd|bash|sh|portable` permite selección explícita;
- una sintaxis que requiere una shell ausente se rechaza en vez de ejecutarse con otra incompatible;
- la salida y el panel TUI muestran la shell realmente usada, y las redirecciones nulas se adaptan a `$null`, `NUL` o `/dev/null`.
- antes de un comando PowerShell se fuerza UTF-8 sin BOM en `[Console]::OutputEncoding` y `$OutputEncoding`; el comando solicitado queda al final para conservar stdout/stderr Unicode y su exit code.


## 10 quinquies. Documentación pública y referencia técnica

- `README.md` presenta Lilith desde la perspectiva del usuario e incluye capturas reales bajo `docs/images/`.
- Las pantallas se documentan mediante tablas HTML con descripciones funcionales, sin mezclar detalles internos.
- `install.md` concentra instalación, compilación, pruebas, releases y explicaciones técnicas del runtime.
- Las rutas de imágenes deben ser relativas al repositorio para que GitHub y el ZIP completo las rendericen sin servicios externos.

## 11. Persistencia y seguridad

- Directorios y archivos sensibles usan permisos restrictivos.
- Secretos nunca deben aparecer en logs ni documentos.
- Los catálogos de modelos no contienen credenciales.
- En Plan se bloquean mutaciones y shell no seguro.
- El OCR marca el texto de imágenes como contenido no confiable.

## 12. Flujo de trabajo

1. Leer `AGENTS.md`, este documento y el último MD de `contexto/`.
2. Revisar `git status` y preservar cambios ajenos a la tarea.
3. Implementar en componentes existentes, sin duplicar runtimes ni estados.
4. Añadir pruebas de regresión.
5. Ejecutar formato, tests, race, vet y builds estáticos/multiplataforma cuando el entorno lo permita.
6. Documentar el cambio en un MD numerado.
7. Commit en español con el autor Git `YahirHub <217099863+YahirHub@users.noreply.github.com>`.
8. Para publicar, cambiar `internal/version/version.go` y ejecutar manualmente el workflow **Publicar release**. Usa un único runner Ubuntu: valida `go mod tidy -diff`, ejecuta pruebas readonly, race/vet y `go mod verify`, compila tests Windows sin ejecutarlos y genera desde Linux los binarios Linux/Windows con `CGO_ENABLED=0`, antes de crear checksums y notas agrupadas. La ejecución nativa de PowerShell 5.1/CMD se valida localmente con `scripts/test.cmd` cuando corresponda. Los instaladores se descargan desde `scripts/` en la rama `main`; Termux compila desde el código en el dispositivo.

## 13. Validación objetivo

```bash
gofmt -w <archivos>
git diff --check
go test -tags=grammar_set_core ./...
go test -tags=grammar_set_core -race ./...
go vet -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -tags=grammar_set_core ./cmd/li
```

El entorno de entrega puede usar stubs locales sólo para comprobar la arquitectura cuando no tenga acceso a módulos o Go 1.25.12; nunca presentar esa comprobación como sustituto de una prueba final con las dependencias oficiales en Windows/Linux/Android. La compatibilidad interactiva de Termux requiere además una prueba en dispositivo ARM64 real.

## 14. Documentos recientes clave

- `081-fix-viewport-config-tview.md`
- `082-compatibilidad-reasoning-inline-y-ocr-estructural.md`
- `083-modelos-conectados-catalogos-modos-y-layout.md`
- `084-catalogos-manuales-sin-endpoint-models.md`
- `085-timeout-shell-solo-explicito.md`
- `086-rendimiento-streaming-y-render-tview.md`
- `087-auto-compactacion-y-comando-compact.md`
- `088-rewind-y-fork-conversacion-codigo.md`
- `089-goal-sin-limites-y-rutas-placeholder.md`
- `090-tests-windows-mcp-y-rewind-eol.md`
- `091-selector-interactivo-destino-fork.md`
- `092-corregir-loop-goal-y-rewind-bloqueado.md`
- `093-vps-red-resiliente-y-releases-manuales.md`
- `094-nombre-codex-ctrl-c-y-autor-git.md`
- `095-corregir-prueba-rewind-en-workflow.md`
- `096-notas-release-e-instaladores.md`
- `097-termux-arm64-agentes.md`
- `098-instaladores-repo-termux-nativo-onboarding.md`
- `099-reconexion-automatica-y-skills-internas.md`
- `100-inteligencia-codigo-estatica.md`
- `101-escritura-atomica-y-guard-heredoc.md`
- `102-shells-nativas-y-semantica-go.md`
- `103-utf8-powershell.md`
- `104-pegado-completo-provider-custom.md`
- `105-runner-dependencias-y-posix-windows.md`
- `106-skill-embebida-metodologia-ponytail.md`
- `107-auditoria-orquestador-y-agentes.md`
- `108-integridad-modulos-y-tests-windows.md`
- `109-estabilizar-prueba-cancelacion-anidada-windows.md`
- `110-documentacion-visual-y-unificacion-historial.md`
- `111-runner-linux-y-go-mod-tidy.md`
- `112-termux-clonado-superficial-y-version-0-2-1.md`
- `113-chatmodel-puntero-go-vet.md`
- `114-busquedas-terminal-acotadas.md`
- `115-compatibilidad-argumentos-str-replace.md`
- `116-ssh-gitzip-boveda-segura.md`
- `117-corregir-colision-helpers-ssh-gitzip.md`
- `118-sesion-boveda-politicas-ssh-widget-permisos.md`
- `119-boveda-permisos-gitzip-autocompletado.md`
- `120-go-sum-y-catalogo-commandcode.md`
- `121-elevacion-segura-ssh-gitzip.md`
- `122-navegador-chromedp-experimental.md`
- `123-corregir-debugger-enable-chromedp.md`
- `124-corregir-arranque-execallocator-chromedp.md`
- `125-corregir-ciclo-vida-contextos-chromedp.md`
- `126-corregir-search-source-ids-caducados.md`
- `127-corregir-compilacion-search-source.md`
- `128-verificar-mapeo-scripts-por-contenido.md`
- `129-shell-portatil-go-y-busqueda-nativa.md`
- `130-corregir-regresiones-shell-portatil.md`
- `131-reintentar-reasoning-content-transitorio.md`
- `132-continuacion-automatica-reasoning-content.md`
- `133-skills-modulares-git-github-docker-frontend.md`
- `134-corregir-fixture-recovery-reasoning.md`
- `135-arquitectura-modulos-estaticos-distribuciones.md`
- `136-migracion-fisica-comandos-a-modulos.md`
- `137-init-knowledge-goal-build.md`
- `138-scripts-instalacion-pruebas.md`
- `139-shell-portatil-propia-sin-mvdan.md`

## 114 · Búsquedas de terminal acotadas y versión 0.2.2

- `code_search` usa ripgrep cuando ya está disponible y cae a un motor Go acotado cuando falta; ripgrep deja de ser una dependencia obligatoria.
- `run_terminal_command` prioriza `code_search` para búsquedas de código y aplica un timeout seguro de 30 segundos a `grep` recursivo, `rg`, `find` y `git grep` cuando no se indicó uno explícito.
- Un `grep -r/-R` simple sin ruta se rechaza antes de crear el proceso y orienta a `code_search` o a una ruta concreta.
- Los comandos complejos con pipes, redirecciones, conectores o saltos de línea no se reescriben.
- Builds, tests e instalaciones conservan ejecución ilimitada cuando `timeout_seconds` se omite.
- La versión central quedó en `0.2.2` para publicar esta corrección.


## 115 · Compatibilidad de argumentos de `str_replace` y versión 0.2.3

- El schema exige `path` más un par completo `old`/`new`, o un `edits[]` no vacío; `old` declara `minLength: 1` para impedir llamadas formadas sólo por la ruta.
- El runtime acepta vocabularios habituales de otros agentes, incluidos `old_string`/`new_string`, `oldText`/`newText`, `search_string`/`replace_string` y variantes equivalentes, tanto en el par simple como dentro de `edits[]`.
- La TUI usa los mismos alias para mostrar el diff real en lugar de un panel `+0`, y la compactación histórica reconoce esos campos para no reenviar cuerpos grandes.
- Omitir el campo de reemplazo se rechaza sin modificar el archivo; una eliminación requiere una cadena vacía explícita.
- La versión central quedó en `0.2.3` para publicar la corrección.


## 116 · SSH persistente, bóveda segura y GitZip; versión 0.3.0

- `ssh_remote` incorpora el registro global de servidores, conexiones persistentes, comandos, directorio remoto, shells PTY y operaciones SFTP compatibles con el flujo de Codewolf.
- Las credenciales guardadas se cifran con scrypt y AES-256-GCM en `~/.li/ssh-secrets.enc`; sólo la bóveda cifrada persiste. El material descifrado permanece en memoria y se elimina junto con todas las conexiones al cerrar Lilith.
- `interaction.Bridge` permite pedir contraseñas o autorizaciones dentro de la TUI sin incluir los valores en el modelo, herramientas, historial o sesiones. Los subagentes reutilizan el mismo canal local.
- `gitzip` crea ZIP/TAR/TAR.GZ locales o remotos, respeta archivos ignore anidados y protege `.git` y `.env`. Las operaciones remotas reutilizan una conexión SSH existente y emplean manifiestos explícitos.
- `/config > Seguridad` controla confirmaciones SSH y protección de archivos `.env`.
- Se añadieron `golang.org/x/crypto v0.54.0` y `github.com/pkg/sftp v1.13.11`; el mínimo pasa a Go 1.25.12 y la versión de Lilith a `0.3.0`.


## 117 · Compilación de SSH y GitZip

- Las nuevas herramientas no redeclaran los helpers históricos `boolArg` e `intArg` del paquete `internal/tools`.
- `boolArgOr` e `intArgOr` viven en `internal/tools/args_optional.go` y conservan defaults, `false`, cero y valores negativos explícitos.
- SSH y GitZip comparten esos helpers sin acoplarse entre sí ni cambiar la semántica de herramientas existentes.
- La versión permanece en `0.3.0` porque esta corrección pertenece al bloque SSH/GitZip aún no publicado.


## 118 · Sesión de bóveda, políticas SSH y permisos dentro del chat

- La bóveda SSH no se desbloquea durante el arranque. Se abre de forma perezosa cuando una conexión necesita una credencial cifrada y permanece abierta únicamente en memoria hasta cerrar Lilith o ejecutar `lock_vault`; las tareas posteriores reutilizan esa sesión.
- `Settings.SSHRemote` reemplaza `SSHSafeMode` con políticas para cambios críticos, cada acción, sólo comandos, confianza total o categorías personalizadas. La migración conserva la intención de configuraciones antiguas sin seguir interrumpiendo cada comando.
- `/config > Seguridad > SSH Remoto` ofrece controles específicos para conexiones, lecturas, comandos, cambios de archivos, eliminaciones, perfiles/credenciales y bloqueo manual de la bóveda.
- Aprobaciones y secretos se muestran como widgets locales en el pie del chat. Los secretos usan entrada enmascarada, textos explícitos según contraseña maestra/servidor/passphrase y nunca entran al historial.
- GitZip deja de solicitar confirmaciones genéricas. Sólo la inclusión explícita de archivos `.env` conserva su autorización separada.

## 119 · Tipos de secreto, permisos con alcance, GitZip selectivo y paleta slash

- Los prompts sensibles usan un `SecretKind` explícito. La contraseña remota ya no puede aparecer rotulada como contraseña maestra sólo porque la explicación mencione la bóveda.
- `EnsureWritable` abre/crea la bóveda antes de solicitar una credencial para guardar; una vez abierta, todas las lecturas y escrituras de esa ejecución reutilizan la misma clave en memoria.
- El widget SSH permite una vez, durante la sesión, siempre para esa acción/destino en el proyecto o denegar. Los permisos permanentes se administran y eliminan desde `/config > Seguridad > SSH Remoto`.
- GitZip acepta una carpeta raíz exacta, selección con `include_paths` y omisiones con `exclude_paths`, tanto local como remotamente.
- La paleta slash prioriza coincidencias exactas, `Tab` agrega un espacio final y las skills tienen tipo/color propio en la paleta y el editor.
- Los `connection_id` son identificadores lógicos estables. `EOF`, ausencia de exit status, `broken pipe` y cierres de red disparan recuperación automática del transporte sin obligar al modelo a cerrar y volver a conectar. Las credenciales solicitadas se conservan sólo en memoria para esa conexión y no vuelven a pedirse al reparar. Un comando ya iniciado no se repite automáticamente si su resultado quedó incierto; los builds remotos no tienen timeout artificial cuando no se especifica uno.

## 120 · Manifiestos Go y catálogo completo de CommandCode

- `go.sum` quedó sincronizado con el resultado de `go mod tidy` de Go 1.25.12 para que la validación `go mod tidy -diff` del workflow sea reproducible.
- El catálogo local cubre explícitamente los 50 IDs publicados por CommandCode el 2026-08-05, incluidos Claude 5/4.8/4.7/4.6, GPT-5.3 Codex, Qwen 3.8/3.7/3.6, Step 3.7/3.5, Gemini Flash/Lite, Fugu Ultra, Inkling y Muse Spark 1.1.
- Los modelos conocidos nunca deben caer en `DefaultMaxContext`; la prueba exhaustiva exige coincidencia normalizada exacta y su ventana declarada.

## 121 · Elevación segura para rutas SSH y GitZip remoto

- Las operaciones remotas de archivo usan `privilege_mode=auto`: prueban SFTP con la cuenta SSH y, ante acceso denegado, preparan el contenido en una ruta temporal y lo publican mediante UID 0, `sudo` o `doas -n`.
- La contraseña sudo tiene tipo de secreto propio, se solicita en un popup local y sólo se entrega por stdin; permanece en memoria durante la conexión lógica y nunca entra en comandos, logs, historial o respuestas.
- Lectura, descarga, listado, stat, escritura, subida, mkdir, renombrado y borrado comparten la capa privilegiada; se preservan propietario/modo al reemplazar y se rechaza eliminar `/`.
- GitZip preflighta lectura y escritura antes de crear o extraer remotamente para elevar el comando completo desde el inicio y evitar efectos parciales.
- Los comandos `exec` arbitrarios sólo se elevan con `privilege_mode=required`; no se reintentan automáticamente después de un error de permisos.


## 129 · Shell portátil Go y búsqueda nativa

- `run_terminal_command` conserva shells nativas como primera opción y usa `portable`, basado en `github.com/YahirHub/go-portable-shell`, cuando falta una shell POSIX o cuando se solicita expresamente.
- La shell embebida aporta un subconjunto Bash/POSIX explícito y un toolbox Go acotado, pero no reemplaza Git, Go, npm, Docker, Make ni otros ejecutables externos. La sintaxis fuera de alcance se rechaza claramente.
- `code_search` usa ripgrep como acelerador opcional y cambia automáticamente a `internal/textsearch` cuando no está disponible.
- Prompts, schemas, selección perezosa y `tool_search` explican capacidades y límites para impedir supuestos falsos del agente.
- Termux deja de instalar ripgrep obligatoriamente; la compilación sigue usando `CGO_ENABLED=0`.

## 139 · Shell portátil propia sin mvdan

- `github.com/YahirHub/go-portable-shell v0.1.0` sustituye a `mvdan.cc/sh/v3`; es un módulo de YahirHub escrito desde cero, sin dependencias, sin CGO y con licencia 0BSD.
- El motor aporta lexer, parser, expansión, funciones, condicionales, bucles, `until`, subshells, pipelines, redirecciones, globbing, aritmética, sustitución acotada, `pipefail`, builtins, ejecutables externos, handlers y cancelación.
- Lilith conserva su toolbox Go mediante la API pública de handlers y la prioridad de ejecutables nativos. La colisión de `find.exe` en Windows continúa resuelta.
- Heredocs, job control, process substitution, arrays y extensiones no implementadas fallan de forma explícita; no se promete compatibilidad Bash completa.
- `THIRD_PARTY_NOTICES.md` se elimina porque sólo documentaba la dependencia retirada. El aviso independiente de OCR bajo `internal/imageocr/NOTICE.md` permanece intacto.
