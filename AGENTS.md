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
- `internal/codeintel/`: detección de host/proyecto, índice sintáctico persistente, Tree-sitter embebido, mapa de repositorio, adaptadores, LSP y SCIP opcionales.
- `internal/plan/`, `internal/goal/`, `internal/todo/`: estados persistentes de Plan, Goal y tareas.
- `contexto/`: decisiones y continuidad técnica numeradas.

## Invariantes que no deben romperse

### Runtime y compilación

- El binario principal debe seguir siendo compatible con `CGO_ENABLED=0`.
- Los builds de Lilith deben incluir `-tags=grammar_set_core`; las gramáticas de `gotreesitter` permanecen comprimidas y embebidas. No usar `grammar_blobs_external`, CGO ni archivos WASM/gramáticas externos en runtime.
- Termux ARM64 se instala compilando nativamente desde el repositorio con Go de Termux; no publicar ni instalar un binario Android no verificado.
- En Termux usar `$PREFIX`, `$HOME`, `PATH` y `pkg`. No asumir `/bin/sh`, `/usr/local/bin`, `sudo`, systemd, glibc ni root.
- Go objetivo: 1.24 o superior.
- `tview` controla el runtime físico; la UI visible se construye con los componentes internos de Lilith.
- No bloquear el bucle de render con llamadas de red o procesos largos: usar `uikit.Cmd`.
- El bucle de estado/streaming nunca debe esperar a `tview.QueueUpdateDraw`: el render físico corre en una cola latest-only independiente y limitada por frecuencia.
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
- `run_terminal_command` debe rechazar antes de ejecutar heredocs incompletos y escrituras inline demasiado largas. El rechazo debe garantizar que no se creó un archivo parcial y orientar a `write_file`/`append_file`.
- En Windows, `run_terminal_command` usa PowerShell para comandos neutrales, CMD para sintaxis CMD y Bash/sh sólo para sintaxis POSIX; en Linux/macOS/Termux usa Bash/sh. No ejecutar silenciosamente un comando con una shell incompatible. El parámetro `shell` permite selección explícita.
- Toda ejecución PowerShell debe forzar UTF-8 sin BOM en `[Console]::OutputEncoding` y `$OutputEncoding` antes del comando. El comando del usuario debe quedar al final para preservar su exit code; no anexar restauraciones u otras sentencias después.
- `write_file` acepta como máximo 1 MiB por llamada. `append_file` acepta 1 MiB por sección, no agrega saltos de línea y el archivo final se limita a 64 MiB; estas reglas deben permanecer visibles en schema, prompt y resultado.

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

### Agentes primarios

- `Tab`: Build → Plan → Goal → Build.
- `Shift+Tab`: ciclo inverso.
- Build permite implementación normal.
- Plan es estrictamente de sólo lectura y conserva su handoff aprobado a Build.
- Goal convierte el texto normal en un objetivo persistente equivalente a `/goal <objetivo>` y puede continuar autónomamente.
- Goal no tiene presupuestos ni límites artificiales de tokens, pasos, turnos o tiempo. El uso y el tiempo son métricas informativas; sólo se detiene por pausa/cancelación del usuario, `blocked`, `complete`, eliminación del goal o un error real del proveedor en la petición actual. Estados antiguos `budget_limited`/`usage_limited` se migran a `active`.
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

## Commits y documentación

- Autor Git local: `YahirHub <217099863+YahirHub@users.noreply.github.com>`.
- Commits en español, detallados y sin mencionar IA.
- Cada cambio importante debe añadir o actualizar un MD numerado en `contexto/`.
- No inventar URLs, repositorios, resultados de pruebas ni compatibilidad no ejecutada.
- Entregar el proyecto completo con `.git` cuando el usuario trabaje reemplazando su copia mediante ZIP.
