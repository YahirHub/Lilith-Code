# AGENTS.md — Lilith Code

## Inicio obligatorio

Antes de modificar el proyecto:

1. Ejecuta `git status --short` y `git log -5 --oneline`.
2. Lee `contexto/000-contexto-maestro.md` y el MD numerado más reciente de `contexto/`.
3. Conserva cualquier cambio previo del usuario que no pertenezca a la tarea; no lo mezcles en el commit.
4. Revisa las pruebas del paquete afectado antes de cambiar su comportamiento.

## Qué es Lilith

Lilith (`li`) es un agente de programación interactivo para terminal, escrito en Go. La TUI usa `tview.Application` sobre Tcell y componentes propios en `internal/tui/uikit`. No debe volver a introducirse Bubble Tea, Bubbles, Lip Gloss, Glamour ni otra capa Charmbracelet.

## Arquitectura esencial

- `cmd/li/`: entrada CLI y arranque de la TUI.
- `internal/tui/`: router de pantallas, chat, streaming, configuración, proveedores, modelos, historial, Plan/Build/Goal y widgets.
- `internal/tui/uikit/`: mensajes, comandos, textarea, textinput, viewport, estilos y ANSI propios.
- `internal/providers/`: configuración de proveedores, conexión, catálogos de modelos y persistencia.
- `internal/providers/openai/`: transportes OpenAI-compatible, Codex y normalización de reasoning.
- `internal/compaction/`: selección del corte, estimación, resumen iterativo y reconstrucción del contexto activo.
- `internal/rewind/`: checkpoints de conversación, snapshots de workspace, restauración y forks aislados.
- `internal/tools/`: herramientas del agente, incluidas shell, archivos, búsqueda, skills, subagentes, OCR e inteligencia de código.
- `internal/skills/` y `assets/skills/`: runtime unificado de Agent Skills y catálogo genérico embebido dentro del binario.
- `internal/codeintel/`: detección de host/proyecto, índice sintáctico persistente, Tree-sitter embebido, mapa de repositorio, adaptadores, LSP y SCIP opcionales.
- `internal/plan/`, `internal/goal/`, `internal/todo/`: estados persistentes de Plan, Goal y tareas.
- `scripts/`: instaladores públicos y helpers locales de validación para Linux, Termux y Windows.
- `contexto/`: decisiones y continuidad técnica numeradas.

## Invariantes que no deben romperse

### Runtime y compilación

- El binario principal debe seguir siendo compatible con `CGO_ENABLED=0`.
- Los builds de Lilith deben incluir `-tags=grammar_set_core`; las gramáticas de `gotreesitter` permanecen comprimidas y embebidas. No usar `grammar_blobs_external`, CGO ni archivos WASM/gramáticas externos en runtime.
- Termux ARM64 se instala compilando nativamente desde el último commit de la rama predeterminada. El instalador debe usar clon superficial (`--depth 1 --single-branch --no-tags`), sin resolver ni fijar tags/commits y sin publicar un binario Android no verificado.
- En Termux usar `$PREFIX`, `$HOME`, `PATH` y `pkg`. No asumir `/bin/sh`, `/usr/local/bin`, `sudo`, systemd, glibc ni root.
- Go objetivo: 1.24 o superior.
- `tview` controla el runtime físico; la UI visible se construye con los componentes internos de Lilith.
- No bloquear el bucle de render con llamadas de red o procesos largos: usar `uikit.Cmd`.
- El bucle de estado/streaming nunca debe esperar a `tview.QueueUpdateDraw`: el render físico corre en una cola latest-only independiente y limitada por frecuencia.
- `ChatModel` contiene atomics y estado concurrente: debe construirse, almacenarse y pasarse siempre como `*ChatModel`. `NewChat` devuelve puntero; no reintroducir retornos, parámetros o asignaciones por valor que copien el modelo.
- No concatenar ni dividir el transcript completo por cada delta. Mantener el historial estable en segmentos de líneas y reconstruir únicamente la cola mutable del turno.

### Inteligencia de código

- `internal/codeintel` debe seguir independiente de la TUI y compartirse entre agente principal/subagentes cuando la raíz coincide.
- El perfil ligero no puede disparar un escaneo completo; el índice se refresca de forma perezosa e incremental al usar sus herramientas.
- El índice vive fuera del repositorio, bajo `~/.li/codeintel/`, se guarda de forma atómica y una cancelación nunca puede reemplazarlo parcialmente ni eliminar registros no visitados.
- Tree-sitter usa `github.com/odvcencio/gotreesitter` en Go puro con gramáticas embebidas. Para lenguajes no disponibles, conservar fallback seguro en lugar de fallar el turno.
- Go debe conservar la capa `go/ast` para constantes, variables, nombres canónicos de paquete y referencias mediante alias; no volver a referencias fuzzy cuando exista identidad calificada.
- `code_context` debe priorizar código fuente relacionado y limitar documentación, scripts de release y tests salvo que la consulta los pida explícitamente.
- `code_graph` debe devolver un subgrafo conectado: seleccionar semillas por la consulta y expandir relaciones vecinas antes de filtrar nodos.
- El perfil `<code_intelligence>` pertenece al mensaje de sistema y nunca debe crear, alterar ni persistir turnos de usuario.
- En Windows nativo, no ejecutar un adaptador Make secundario cuando el Makefile use asignaciones/utilidades POSIX o exista un adaptador principal compatible.
- LSP y SCIP son capas opcionales: sólo usar ejecutables e índices ya presentes, nunca instalar o descargar componentes por cuenta propia. No incrustar ni descargar `gopls`; cuando no esté disponible, `code_semantic` para Go debe usar el fallback estático interno basado en índice/go parser.
- `code_format_validate` sólo puede modificar rutas explícitas dentro de la raíz; rechazar escapes `..` y no formatear un workspace completo cuando `changed_paths` esté vacío.
- Las validaciones deben derivarse de manifests y herramientas reales del proyecto, y no inventar package managers o comandos.

### Escritura de archivos y terminal

- `write_file` y `append_file` escriben contenido estructurado directamente desde Go; nunca deben envolverse internamente en heredocs, `printf`, PowerShell here-strings, base64 ni otro comando de shell.
- Toda escritura completa o append se materializa primero en un archivo temporal del mismo directorio, se sincroniza, verifica por bytes/SHA-256 y se publica de forma atómica. Una cancelación o fallo no puede dejar el destino truncado.
- `create_file` conserva semántica create-only incluso ante carreras: si el destino aparece después del preflight, la publicación debe fallar sin sobrescribirlo.
- Reemplazar un archivo existente con `write_file` requiere `overwrite=true`; cuando el contenido se leyó antes, usar `expected_sha256` para detectar cambios observables. Para documentos largos, `append_file` debe recibir secciones completas y acotadas, reutilizando el SHA devuelto cuando sea posible.
- `str_replace` y `apply_diff` siguen siendo la vía preferida para cambios localizados. Un mismatch debe incluir hash, tamaño, líneas y una región actual cercana; nunca aplicar reemplazo fuzzy destructivo por cuenta propia.
- `str_replace` requiere `path` más ambos campos `old`/`new`, o un `edits[]` no vacío. El runtime y la TUI normalizan alias comunes de agentes compatibles (`old_string`/`new_string`, `oldText`/`newText`, `search_string`/`replace_string` y variantes camel/snake). Nunca interpretar la ausencia de reemplazo como eliminación; sólo una cadena vacía explícita puede borrar el target.
- `run_terminal_command` debe rechazar antes de ejecutar heredocs incompletos y escrituras inline demasiado largas. El rechazo debe garantizar que no se creó un archivo parcial y orientar a `write_file`/`append_file`.
- En Windows, `run_terminal_command` usa PowerShell para comandos neutrales, CMD para sintaxis CMD y Bash/sh sólo para sintaxis POSIX; las asignaciones `VAR=value` deben detectarse tanto antes de un comando como antes de `;`, salto de línea o fin de entrada. En Linux/macOS/Termux usa Bash/sh. Si no existe una shell POSIX compatible, debe usar la shell portátil en Go (`shell=portable`) como último respaldo, sin desplazar una shell nativa disponible. La shell portátil usa `github.com/YahirHub/go-portable-shell`, interpreta un subconjunto Bash/POSIX explícito y ofrece un conjunto acotado de comandos Go, pero no reemplaza ejecutables externos como Git, Go, npm, Docker o Make. Heredocs, job control, process substitution, arrays y sintaxis no soportada deben fallar claramente, nunca reinterpretarse. No ejecutar silenciosamente un comando con una shell incompatible. El parámetro `shell` permite selección explícita.
- Toda ejecución PowerShell debe forzar UTF-8 sin BOM en `[Console]::OutputEncoding` y `$OutputEncoding` antes del comando. El comando del usuario debe quedar al final para preservar su exit code; no anexar restauraciones u otras sentencias después.
- Las búsquedas de código deben usar `code_search` cuando sea posible. `code_search` usa `rg` cuando ya está disponible y cae de forma transparente a un motor Go acotado; nunca instalar ni descargar ripgrep sólo para inspeccionar el repositorio. `run_terminal_command` aplica un límite de 30 segundos a búsquedas de repositorio (`grep -r/-R`, `rg`, `find`, `git grep`) cuando el modelo omite `timeout_seconds`; builds, tests e instalaciones siguen sin límite implícito. Un `grep` recursivo simple sin destino explícito se rechaza antes de crear el proceso y orienta a `code_search` o a una ruta concreta; no preflight-parsear ni reescribir comandos complejos con pipes, redirects o conectores.
- Toda dependencia directa debe llegar con sus checksums de contenido y `go.mod` versionados en `go.sum`. CI ejecuta `go mod tidy -diff`, después pruebas con `-mod=readonly` y finalmente `go mod verify`; no usar `go mod download all`, porque amplía innecesariamente las descargas a módulos ajenos a la compilación. Nunca desactivar `GOSUMDB` para ocultar errores de integridad. El workflow de release usa un único runner `ubuntu-latest`; compila los artefactos Windows con `GOOS=windows`, `GOARCH` correspondiente y `CGO_ENABLED=0`, y compila tests sensibles para Windows sin intentar ejecutarlos en Linux. La ejecución nativa de PowerShell 5.1/CMD se valida localmente con `scripts/test.cmd` cuando una modificación dependa de comportamiento exclusivo de Windows.
- `write_file` acepta como máximo 1 MiB por llamada. `append_file` acepta 1 MiB por sección, no agrega saltos de línea y el archivo final se limita a 64 MiB; estas reglas deben permanecer visibles en schema, prompt y resultado.

### Skills

- Las skills embebidas deben usar el mismo loader, precedencia y herramientas `skill_read`/`skill_search`/`skill_files` que las skills externas; no crear un segundo sistema de prompts internos.
- `ponytail-development` es una metodología genérica de desarrollo, no documentación de instalación, compilación o releases de Lilith. Conservar su contenido completo salvo que el usuario actualice explícitamente la metodología.
- `SkillsEnabled` es el interruptor maestro. `DisabledSkills` sólo guarda excepciones por nombre, normalizadas en minúsculas; una skill nueva debe quedar habilitada por defecto.
- Una skill desactivada individualmente no puede aparecer en activación automática, paleta, agentes ni invocación manual. El catálogo crudo debe seguir disponible en `/config > Skills` para poder reactivarla.
- La precedencia continúa siendo proyecto > usuario > embebida por `name`; desactivar un nombre afecta a la implementación efectiva que gane esa precedencia.

### SSH, bóveda y GitZip

- `ssh_remote` es el único runtime de SSH. Perfiles, conexión directa, reanudación por `connection_id`, SFTP, shell y bóveda deben converger en `internal/sshremote`; no crear comandos paralelos que abran procesos `ssh` externos.
- Un `connection_id` es una conexión lógica estable y no debe reemplazarse ante `EOF`, `broken pipe`, ausencia de exit status o cierre del transporte. `internal/sshremote` debe vigilar, marcar y reconectar el transporte automáticamente conservando el mismo ID. No repetir a ciegas comandos ya iniciados: devolver `exit_status_known=false` y una indicación estructurada para verificar su efecto. Los builds/despliegues remotos no deben recibir un timeout artificial cuando `timeout_seconds` se omite.
- Los perfiles persistentes nunca guardan contraseñas ni passphrases literales. Las credenciales elegidas por el usuario se cifran en `ssh-secrets.enc`; la contraseña maestra y la clave derivada sólo viven en memoria y `sshremote.ShutdownAll()` debe ejecutarse al cerrar el CLI.
- Toda solicitud de contraseña se realiza mediante `interaction.Bridge` y un popup enmascarado dentro del chat. El popup nunca forma parte del transcript. No serializar secretos en tool calls, sesiones, logs, eventos de agentes o respuestas del modelo. Una contraseña/passphrase solicitada para conexión directa puede conservarse únicamente dentro de la conexión lógica en memoria para reparar el transporte sin volver a pedirla, y debe borrarse al cerrar esa conexión.
- Una bóveda SSH existente se desbloquea de forma perezosa sólo cuando una acción necesita una credencial cifrada. Después conserva su clave descifrada únicamente en memoria hasta `lock_vault` o el cierre del proceso. Guardar nuevas contraseñas/passphrases debe reutilizar la bóveda abierta y nunca volver a pedir la contraseña maestra durante la misma ejecución. Cada prompt lleva un tipo explícito (`vault_master`, `remote_password`, `sudo_password` o `key_passphrase`); no inferirlo buscando palabras en el mensaje.
- Las aprobaciones SSH se rigen por `Settings.SSHRemote` y `/config > Seguridad > SSH Remoto`: cada acción pertenece a una categoría y las políticas disponibles son cambios críticos, cada acción, sólo comandos, confiar en el modelo o una matriz personalizada. El widget permite denegar, permitir una vez, permitir para la sesión o persistir esa acción/destino para el proyecto. Los permisos persistidos deben poder limpiarse desde la misma pantalla. Confirmaciones y secretos nunca entran al transcript.
- Las operaciones de archivo remoto prueban primero SFTP con la cuenta SSH. Ante una ruta protegida, deben preparar el contenido en una ruta temporal escribible y publicarlo mediante UID 0, `sudo` o `doas -n`; una contraseña sudo sólo viaja por stdin y vive en memoria dentro de la conexión lógica. Nunca insertarla en el comando, argumentos, logs o resultados. Conservar propietario/modo al reemplazar y rechazar la eliminación de `/`. `privilege_mode=never` prohíbe elevar y `required` eleva desde el inicio.
- Los comandos `exec` arbitrarios no se repiten automáticamente con sudo después de fallar, porque podrían haber aplicado efectos parciales. Sólo `privilege_mode=required` puede elevar el comando completo desde el inicio. GitZip debe preflightar lectura/escritura y decidir la elevación antes de crear o extraer un archivo.
- GitZip no solicita una confirmación genérica por crear, subir, construir o extraer archivos. Los `.env` reales permanecen protegidos por `ProtectEnvFiles` y requieren una aprobación separada sólo cuando se solicita incluirlos.
- `gitzip` usa el backend común de `internal/gitzip` para archivos locales y el mismo matcher para manifiestos remotos. `source_path` define la carpeta raíz exacta, `include_paths` limita el manifiesto y `exclude_paths` agrega omisiones compatibles con gitignore. Siempre excluir `.git`, salida, temporales y secretos protegidos; conservar reglas anidadas y negaciones.
- Los subagentes heredan el puente de secretos y confirmaciones del padre. Las conexiones y la bóveda son globales al proceso, no a una conversación ni a un agente.

### Orquestador y subagentes

- `Agent`, `Task`, `task` y `agent` deben converger en el mismo contrato; no crear runtimes paralelos para invocación manual, tool calls o agentes anidados.
- Un lote compuesto sólo por llamadas `Agent` puede ejecutarse concurrentemente, pero los mensajes `tool` deben conservar el orden de las llamadas originales. Hooks y checkpoints mutables deben serializar sus fronteras.
- El árbol de agentes hereda cancelación: cancelar el padre detiene hijos anidados; `/clear`, cambiar de sesión y salir cancelan todo background de la sesión anterior. Los eventos de una generación anterior nunca pueden entrar al chat nuevo.
- Todo agente background debe producir exactamente un evento terminal visible aunque falle antes de crear la sesión hija. Sus finalizaciones se persisten y se entregan una sola vez al modelo padre en la siguiente solicitud, antes del prompt actual del usuario.
- Si `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`, una solicitud marcada background debe convertirse por completo a foreground: permisos, eventos y resultado final; no basta con esperar el mismo worker con política background.
- Reanudar mediante `task_id` usa el provider/modelo guardados en la sesión hija. Si el provider desapareció, el modelo está vacío o el worktree ya no existe, fallar explícitamente; nunca heredar silenciosamente la selección actual ni volver al checkout principal.
- Una sesión hija no puede ejecutarse o reanudarse dos veces simultáneamente dentro del proceso. La persistencia usa temporales únicos, permisos restrictivos, `Sync` y reemplazo; no compartir un nombre `.tmp` entre workers.
- El perfil `<code_intelligence>` de un subagente debe contener exactamente un bloque de apertura/cierre y permanecer en el mensaje de sistema.
- Los eventos terminales son idempotentes en la TUI: un replay no duplica errores, salida ni notificaciones.

### Proveedores y modelos

- `/providers` puede mostrar proveedores configurados aunque estén desconectados para permitir iniciar sesión o corregir credenciales.
- `/models` sólo puede listar proveedores cuya conexión esté disponible:
  - `bundled` y `none`: conectados;
  - `api_key`: requiere clave guardada;
  - `env`: requiere la variable definida;
  - `oauth`: requiere sesión OAuth guardada.
- Nunca se puede activar un proveedor desconectado.
- El proveedor OAuth integrado usa el nombre visible corto `ChatGPT Codex`; no volver a mostrar `ChatGPT Plus/Pro (Codex)` como nombre del proveedor.
- Al abrir `/models`, se refresca en segundo plano el endpoint `/models` de todos los proveedores conectados. `Ctrl+R` fuerza otra actualización sin impedir escribir la letra `r` en el filtro.
- Los catálogos custom se persisten en `providers.json`.
- Los catálogos bundled se guardan en `provider-model-cache.json`; así los modelos descubiertos sobreviven a reinicios y trabajo sin red.
- Si el modelo activo desaparece del catálogo, seleccionar el primer modelo válido de un proveedor conectado.
- Un fallo de un proveedor no debe impedir actualizar los demás ni borrar su última caché válida.
- Los errores de transporte (`dial tcp`, DNS, timeout, EOF, conexión reiniciada o stream truncado) no deben cerrar el turno ni mostrarse crudos: comprobar conectividad, mantener una espera cancelable con `Esc` y reintentar la misma solicitud cuando el endpoint vuelva.
- Si un stream ya mostró contenido antes del corte, descartar sólo la respuesta parcial del request actual antes de reintentar; nunca duplicar texto, reasoning ni tool calls y nunca borrar el prompt del usuario.
- Distinguir errores de red de errores definitivos de autenticación, payload o configuración. HTTP 4xx no recuperables deben seguir fallando de forma explícita.
- Si `GET {baseURL}/models` responde 404, 405 o 501, tratar el catálogo como no soportado: conservar modelos manuales/caché y no mostrarlo como error.
- Los campos de URL/API key del proveedor personalizado deben usar el ancho interior real de la caja y conservar una ventana horizontal alrededor del cursor. Renderizar una línea larga nunca puede truncar el valor almacenado ni imponer de nuevo el límite heredado de 2,048 runes.

### Agentes primarios

- `Tab`/`Shift+Tab`: alternan Goal ↔ Build. Plan se selecciona con `/plan`; desde Plan, Tab vuelve a Build.
- Build permite implementación normal.
- Plan es estrictamente de sólo lectura y conserva su handoff aprobado a Build.
- Goal convierte el texto normal en un objetivo persistente equivalente a `/goal <objetivo>`; al enviarlo, el selector vuelve automáticamente a Build y la ejecución autónoma usa sus herramientas.
- Goal no tiene presupuestos ni límites artificiales de tokens, pasos, turnos o tiempo. El uso y el tiempo son métricas informativas; se detiene por pausa/cancelación del usuario, `blocked`, `complete`, eliminación del goal o un error real. Un cierre de sesión, recuperación de un goal que estaba activo o fallo definitivo lo deja `interrupted`, visible con la acción Continuar. Estados antiguos `budget_limited`/`usage_limited` se migran a `active`.
- `goal_complete` es la acción explícita del modelo para completar y guardar un resumen; `/resume` reabre estados pausados, bloqueados, interrumpidos o completados sin cambiar el objetivo. En Build, `continue`, `continuar` o `resume` reanudan localmente un goal interrumpido y no se envían como texto literal al proveedor.
- Un turno ya iniciado conserva el modo que tenía al comenzar; cambiar con Tab sólo afecta al siguiente turno.

### Compactación de contexto

- La auto compactación se evalúa antes de cada request del proveedor y se activa cuando mensajes + schemas de herramientas alcanzan `ventana - reserva`.
- Reserva predeterminada: 16,384 tokens; si una ventana declarada es menor que la reserva se usa un fallback seguro. El tail exacto usa hasta 20,000 tokens y se reduce a `contextWindow/4` en modelos pequeños. Conservar dos turnos recientes completos cuando caben.
- `/compact [instrucciones opcionales]` fuerza la misma compactación cuando el agente está en reposo; si el historial completo cabe en la cola normal, resume todos los turnos anteriores y conserva exacta la solicitud más reciente.
- El resumen anterior se entrega como contexto iterativo en compactaciones posteriores; nunca resumir el resumen como una conversación ordinaria.
- El transcript visual no se recorta. Los mensajes eliminados del contexto activo se archivan exactamente en `Session.Compactions` para auditoría y conteo de turnos.
- No dividir pares assistant tool-call / tool result. El tail empieza normalmente en un usuario; un turno individual enorme puede empezar en una frontera de assistant, nunca en un resultado `tool`. Si no cabe ninguna frontera segura, resumir el contexto activo completo y archivar los originales.
- Si el proveedor devuelve overflow de contexto, compactar y reintentar sobre ese estado. Tras una compactación exitosa volver a evaluar una vez: la sobrecarga de system prompt/schemas puede exigir reducir de dos turnos exactos a uno. Detenerse cuando ya no exista historial anterior reducible.
- La solicitud de resumen no expone herramientas ni continúa la tarea: debe devolver sólo un handoff estructurado. Acotar tool outputs y también el prompt total para que una cantidad patológica de mensajes pequeños no exceda la ventana.

### Rewind y forks

- Antes de cada nueva acción del usuario se crea un checkpoint de conversación. El workspace se captura de forma perezosa inmediatamente antes de la primera herramienta, hook o subagente que pueda mutarlo.
- `/rewind` ofrece tres restauraciones: código + conversación, sólo conversación o sólo código. Restaurar conversación mantiene el ID de la línea temporal activa y devuelve el prompt elegido al editor. Sólo se permite cuando no hay turno, comando directo ni subagente background en ejecución.
- Antes de una restauración destructiva se crea un punto de seguridad con conversación y archivos actuales, para poder deshacer el propio rewind.
- En repositorios Git, los snapshots usan un índice temporal, un commit interno y `refs/lilith/rewind/...`; nunca modificar el índice/staging real del usuario. En monorepos, la restauración se limita al subdirectorio de proyecto activo y no toca workspaces hermanos.
- Las operaciones Git que capturan o materializan contenido (`add`, `checkout-index`, `worktree add`) deben ignorar el `core.autocrlf` global del equipo para no cambiar bytes LF/CRLF durante rewind o fork. Las pruebas de rutas deben comparar rutas limpias semánticamente, no separadores literales de un sistema operativo.
- Fuera de Git, usar blobs por SHA-256 y manifiestos. Los snapshots parciales deben advertirlo y nunca prometer restauración exacta de rutas excluidas o archivos omitidos.
- `/fork [título]` abre primero un navegador de carpetas propio de la TUI. Debe funcionar con teclado en SSH y con clic/rueda cuando el terminal reporte mouse; permite volver al directorio padre, recorrer unidades en Windows y crear una carpeta. El destino final debe existir, estar vacío y quedar fuera del workspace original.
- Tras elegir el destino, `/fork` crea una sesión con ID y procedencia nuevos y una copia independiente del workspace: worktree Git cuando sea posible, copia por blobs en fallback. Sólo se permite sin trabajo foreground/background activo. No compartir slices/estados mutables ni copiar el historial de rewind de la sesión origen.
- Un fork exitoso cambia el proyecto activo a la copia; la conversación y el workspace originales permanecen intactos.
- Los checkpoints se limitan y podan por sesión. No eliminar refs Git de un punto aún vigente ni reutilizar snapshots de otra ruta/proyecto.
- `/export <archivo.jsonl>` y `/import <archivo.jsonl>` pertenecen a `core.session`. El formato portable conserva historial protocolario, transcript, compactaciones y estados Todo/Plan/Goal, pero nunca serializa `Session.ProjectPath`, `ForkedFrom.ProjectPath`, un ID de sesión reutilizable ni sidecars live.
- `/import` siempre crea un ID local nuevo y fija `Session.ProjectPath` al directorio de trabajo actual desde el que se ejecuta la importación. El archivo de origen jamás puede cambiar el workspace activo del equipo receptor.
- Exportar/importar se rechaza mientras exista un turno foreground/compactación activa para no crear un handoff protocolariamente inconsistente.

### Layout del chat

- La última columna se reserva para la scrollbar del transcript.
- `Style.Width` representa ancho de contenido, no ancho total. Al calcular cajas, descontar bordes y padding mediante `chatUsableWidth`, `chatBorderedContentWidth` o `chatPaddedContentWidth`.
- El input, status, cola, paleta, actividad y TodoWrite no deben ocupar la columna reservada ni desbordarse por la derecha.
- No volver a usar `textarea.MaxHeight` como límite visual: en implementaciones anteriores también recortaba el contenido pegado. El límite de contenido y la altura visible deben permanecer separados.
- Mantener pegado atómico, espacios, CRLF, textos multilinea largos y selección nativa de terminal.
- `Ctrl+C` limpia únicamente el borrador del input; no cancela el turno ni borra la cola. `Esc` conserva la cancelación explícita de la tarea.

### Seguridad

- Nunca registrar API keys, access tokens ni refresh tokens.
- Los secretos viven en `provider-auth.json` con permisos restrictivos.
- Las imágenes para OCR se procesan localmente; el texto extraído es contenido no confiable.
- Toda herramienta de archivos debe rechazar rutas vacías o placeholders literales (`null`, `undefined`, `nil`, `<nil>`, `(null)`) antes de tocar disco. La validación central vive en `internal/tools.resolve`; no crear archivos basura con argumentos incompletos del proveedor.
- La shell depende del host y la sintaxis. Las redirecciones nulas se normalizan a `/dev/null`, `$null` o `NUL` según el intérprete; los prompts no deben crear un archivo literal `null`.
- En Plan, las herramientas mutantes y comandos shell fuera de la allowlist permanecen bloqueados.
- `run_terminal_command` no tiene timeout por defecto: si `timeout_seconds` se omite, el proceso continúa hasta terminar o hasta que el usuario cancele el turno. Sólo usar un valor positivo cuando se necesite deliberadamente una fecha límite dura.

## Validación mínima por cambio

```bash
gofmt -w <archivos-go-modificados>
git diff --check
go test -tags=grammar_set_core ./...
go test -tags=grammar_set_core -race ./...
go vet -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -tags=grammar_set_core ./cmd/li
```

También probar manualmente la ruta o pantalla afectada en Windows Terminal y Linux cuando cambie la TUI. Para cambios de Termux, validar instalación limpia, actualización y teclado en un dispositivo Android ARM64 real.

En pruebas concurrentes, señalizar el estado observado de forma síncrona en la frontera real (por ejemplo, al entrar a `Stream`), no desde una goroutine auxiliar cuya planificación dependa del sistema operativo. Separar los límites de arranque y cancelación, y detectar la terminación prematura para que un timeout no oculte el error original.

## Commits y documentación

- Autor Git local: `YahirHub <217099863+YahirHub@users.noreply.github.com>`.
- Commits en español, detallados y sin mencionar IA.
- Si una implementación nueva necesita correcciones antes de quedar aceptada, reescribir/amendar ese commit y dejar un único commit limpio de la funcionalidad. No registrar commits ni MD de `contexto/` dedicados a errores transitorios de esa misma implementación.
- Cada cambio importante debe añadir o actualizar un MD numerado en `contexto/`.
- No inventar URLs, repositorios, resultados de pruebas ni compatibilidad no ejecutada.
- Entregar el proyecto completo con `.git` cuando el usuario trabaje reemplazando su copia mediante ZIP.

### Documentación pública

- `README.md` debe explicar qué puede hacer Lilith desde la perspectiva del usuario. Evitar allí detalles internos extensos de paquetes, locks, checksums, persistencia o implementación.
- `install.md` concentra instalación, compilación, pruebas, releases y referencia técnica del runtime.
- Las capturas públicas viven en `docs/images/` con nombres estables. Cuando se presenten varias pantallas, usar tablas HTML legibles con descripciones funcionales y texto alternativo; no enlazar imágenes desde rutas temporales externas.

## 120 · Manifiestos Go y catálogo CommandCode

- `go.sum` debe permanecer exactamente sincronizado con `go mod tidy` de Go 1.25.12; el workflow usa `go mod tidy -diff` y cualquier diferencia es un fallo real.
- Todo modelo publicado por `/models` de CommandCode debe tener una entrada explícita en `internal/models/catalog.go`; no depender de `DefaultMaxContext` ni de coincidencias parciales para IDs conocidos.
- Los prefijos de proveedor y sufijos de fecha/preview/free se normalizan, pero la clave normalizada de cada modelo soportado debe resolver de forma exacta.
- La prueba `TestCommandCodeCatalogHasExplicitContextForPublishedModels` es la lista de regresión del catálogo recibido el 2026-08-05.

## 125 · Ciclo de vida del navegador Chromedp

- El primer `chromedp.Run` de un navegador o target debe ejecutarse sobre el contexto persistente creado por `chromedp.NewContext`; nunca envolver ese primer `Run` en `context.WithTimeout`, porque cancelar el hijo destruye el executor y las siguientes acciones fallan con `context canceled`.
- Los límites del primer arranque se implementan con `runInitial`, que cancela el contexto persistente únicamente si el arranque realmente excede el tiempo máximo.
- Los contextos temporales con timeout sólo se usan después de que el navegador o target haya completado su primer `Run`.
- Una sesión debe sobrevivir a llamadas separadas de herramienta con el mismo `session_id`: como mínimo probar `start -> navigate -> snapshot -> status -> screenshot`.
- `status` nunca debe ocultar un error CDP ni devolver `tabs: null`; debe exponer `cdp_error`, `attached=false` y una lista vacía cuando la conexión no responda.
- Los comandos del dominio `Target` deben ejecutarse con el executor del navegador (`chromedp.Targets` o `cdp.WithExecutor(..., state.Browser)`); Network, Runtime y Debugger usan el executor del target.
- La cancelación del contexto de una llamada puede detener su acción hija, pero nunca debe cancelar el contexto persistente guardado en la sesión.
- `browser action=profiles` puede enumerar nombres/directorios de perfiles locales, pero no debe leer ni exponer cookies, contraseñas, tokens, `user_name` u otros datos de cuenta durante el descubrimiento.
- `profile_mode=existing` sólo reutiliza un perfil personal cuando la selección fue explícita y existe un `DevToolsActivePort` local activo; usar el WebSocket completo directamente y no depender de `/json/version`, porque el flujo de aprobación de Chrome moderno puede ser WebSocket-only. No relanzar ni modificar a la fuerza el `User Data` personal para sortear las restricciones de Chrome.
- La importación de cookies es file-backed: el modelo sólo pasa `cookie_path`; valores y contenido completo del JSON permanecen locales y nunca deben aparecer en argumentos visibles, logs, transcript, errores o respuesta. El resultado se limita a contadores.
- Las cookies particionadas no se aplanan a cookies normales. Si el formato no puede conservar su semántica de aislamiento, se omiten explícitamente.

## Static module architecture

- Slash-command extensions belong behind `internal/moduleapi`; private/company modules must not import `internal/tui` or access `ChatModel` fields directly.
- Public built-ins are selected from `internal/distribution/builtin.go`. A private downstream distribution should add its own build-tagged file (for example `internal/distribution/company.go` with `//go:build company`) instead of editing the public selector.
- Public slash capabilities live physically under `modules/core/**`; do not reintroduce a centralized `core.commands` table in `internal/tui/commands.go`. The TUI only adapts `moduleapi.Host` capabilities and materializes the registry.
- New public user-facing slash functionality should normally be a focused `core.<feature>` module. Shared libraries/services remain under `internal/**` and are reached through small capability interfaces instead of importing `internal/tui` from a module.
- New company modules should live under new paths such as `modules/company/**`. This keeps `merge upstream/main` low-conflict.
- Module IDs, commands, aliases and routes are unique. The registry fails closed on collisions, missing required modules or incompatible `moduleapi.APIVersion`; do not bypass that validation.
- Use `/modules` to inspect linked modules and diagnostics.
- `go run ./cmd/build build --distribution company` adds the company build tag while preserving `CGO_ENABLED=0` and the embedded grammar tag.
