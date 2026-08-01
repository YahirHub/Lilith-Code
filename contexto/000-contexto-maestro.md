# Contexto maestro de Lilith Code

> Estado consolidado para retomar el proyecto sin depender del historial del chat. Antes de trabajar, leer también `AGENTS.md`, el documento numerado más reciente de `contexto/`, `git status` y los últimos commits.

## 1. Producto

Lilith (`li`) es un agente de programación interactivo para terminal, implementado en Go. Incluye chat con streaming, tool calls, edición de archivos, shell, skills, subagentes, MCP, plugins compatibles con Claude, historial persistente, modos Build/Plan/Goal, tareas, goals durables, búsqueda web y OCR estructural local.

El proyecto conserva un diseño inspirado en agentes de terminal modernos, pero su implementación actual es propia.

## 2. Stack vigente

- Go 1.24+.
- `tview v0.42.0` como runtime interactivo.
- Tcell como backend de pantalla, teclado, ratón y pegado.
- Widgets y ciclo lógico propios en `internal/tui/uikit`.
- `rivo/uniseg` para ancho Unicode.
- Cobra para la CLI.
- Binario objetivo con `CGO_ENABLED=0`.

No quedan dependencias de Bubble Tea, Bubbles, Lip Gloss, Glamour ni otros módulos Charmbracelet. No deben reintroducirse.

## 3. Estructura principal

```text
cmd/li/                       entrada CLI
internal/config/              ajustes persistentes
internal/secrets/             API keys y OAuth
internal/providers/           proveedores, conexión y catálogos
internal/providers/openai/    chat completions, Responses/Codex, reasoning
internal/compaction/           auto compactación y resumen iterativo del contexto
internal/rewind/               checkpoints, restauración de código y forks de workspace
internal/tui/                 chat y pantallas interactivas
internal/tui/uikit/           componentes TUI propios
internal/tools/               herramientas del agente
internal/plan/                estado y políticas Plan
internal/goal/                objetivos persistentes
internal/todo/                TodoWrite persistente
internal/subagents/           ejecución de subagentes
internal/imageocr/            OCR nativo Windows y modelo estructural
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
- el transcript conserva el historial estable como segmentos de líneas y sólo vuelve a procesar la cola mutable, evitando trabajo proporcional a toda la conversación por cada token.

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

Los modelos nuevos de proveedores custom se guardan en `providers.json`. Los de proveedores bundled se guardan en `provider-model-cache.json`, por lo que permanecen disponibles tras cambiar de pantalla, reiniciar o perder temporalmente la conexión.

## 6. Modos Build, Plan y Goal

`Tab` recorre Build → Plan → Goal; `Shift+Tab` recorre al revés. El modo elegido aplica al próximo mensaje, mientras un turno en ejecución conserva su snapshot.

- **Build:** implementación normal y herramientas mutantes.
- **Plan:** sólo lectura; puede investigar, preguntar decisiones y entregar un plan. El cambio Plan → Build puede consumir una vez el plan aprobado.
- **Goal:** el texto introducido se convierte en objetivo persistente, igual que `/goal <objetivo>`, y arranca o reorienta una ejecución autónoma.

Los estados se persisten en la sesión. Goal comparte las capacidades de implementación de Build; Plan conserva su política restrictiva.

## 7. Chat y ejecución

- Streaming SSE/Responses con normalización por proveedor.
- Reasoning separado del mensaje final, incluidos campos estructurados y etiquetas inline como `<think>`.
- Tool calls con paneles en vivo y persistentes.
- `run_terminal_command` no impone límite de ejecución cuando `timeout_seconds` no está presente. Los builds, instalaciones y pruebas largas siguen ejecutándose hasta completar o hasta una cancelación explícita; un timeout positivo conserva el corte y la limpieza del árbol de procesos.
- Cola de steering y follow-up sin abrir turnos paralelos.
- Cancelación con Esc; `/exit` es la salida explícita.
- TodoWrite, planes y goals se guardan en la sesión.
- Skills y agentes pueden usar modelo heredado, explícito o lista de preferencias.
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
- antes de efectuar el rewind se crea un punto de seguridad del estado actual, de modo que la propia restauración pueda revertirse desde `/rewind`;
- se conservan como máximo 80 puntos por sesión. Los puntos anteriores a la introducción de esta función no pueden reconstruirse retroactivamente.

En Git, el snapshot usa un índice temporal separado del índice real, crea un commit interno y lo fija bajo `refs/lilith/rewind/<sesión>/<punto>`. El staging del usuario no se altera. Al restaurar se materializa únicamente el path del proyecto activo y se eliminan dentro de ese scope los archivos tracked o untracked no ignorados que no existían en el punto. En monorepos, directorios hermanos quedan intactos. Los archivos ignorados generados quedan fuera salvo que ya estuvieran tracked.

Fuera de Git, se usa un manifiesto con blobs SHA-256. Se excluyen `.git`, `.lilith`, `.cache`, `node_modules`, `.next`, `dist`, `build` y `target`; cada archivo está limitado a 32 MiB y el snapshot a 512 MiB. Un punto parcial sigue permitiendo restaurar lo capturado, pero la UI debe advertirlo.

`/fork [título opcional]` captura el estado actual y crea una sesión independiente con nuevo ID y `ForkedFrom`. Se rechaza mientras haya un turno, comando directo o subagente background activo. Para Git materializa un worktree separado en el commit del snapshot; en fallback reconstruye una copia independiente desde los blobs. Lilith cambia al nuevo directorio y reconecta MCP. La sesión original, sus archivos y su historial de rewind permanecen intactos; el fork no hereda checkpoints antiguos.

## 10. OCR estructural

`extract_image_text` permite a modelos sin visión procesar capturas y documentos sin subir la imagen:

- Windows: `Windows.Media.Ocr` mediante WinRT en Go puro.
- Otros sistemas/fallback: Tesseract externo opcional.
- Salidas: texto, layout monoespaciado, regiones, separadores, coordenadas y JSON.
- Mantiene `CGO_ENABLED=0` porque no enlaza una biblioteca OCR al binario.

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
7. Commit en español con el autor Git del usuario.

## 13. Validación objetivo

```bash
gofmt -w <archivos>
git diff --check
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/li
```

El entorno de entrega puede usar stubs locales sólo para comprobar la arquitectura cuando no tenga acceso a módulos o Go 1.24; nunca presentar esa comprobación como sustituto de una prueba final con las dependencias oficiales en Windows/Linux.

## 14. Documentos recientes clave

- `081-fix-viewport-config-tview.md`
- `082-compatibilidad-reasoning-inline-y-ocr-estructural.md`
- `083-modelos-conectados-catalogos-modos-y-layout.md`
- `084-catalogos-manuales-sin-endpoint-models.md`
- `085-timeout-shell-solo-explicito.md`
- `086-rendimiento-streaming-y-render-tview.md`
- `087-auto-compactacion-y-comando-compact.md`
- `088-rewind-y-fork-conversacion-codigo.md`
