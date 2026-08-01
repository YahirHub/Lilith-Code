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
- `internal/tools/`: herramientas del agente, incluidas shell, archivos, búsqueda, skills, subagentes y OCR.
- `internal/plan/`, `internal/goal/`, `internal/todo/`: estados persistentes de Plan, Goal y tareas.
- `contexto/`: decisiones y continuidad técnica numeradas.

## Invariantes que no deben romperse

### Runtime y compilación

- El binario principal debe seguir siendo compatible con `CGO_ENABLED=0`.
- Go objetivo: 1.24 o superior.
- `tview` controla el runtime físico; la UI visible se construye con los componentes internos de Lilith.
- No bloquear el bucle de render con llamadas de red o procesos largos: usar `uikit.Cmd`.
- El bucle de estado/streaming nunca debe esperar a `tview.QueueUpdateDraw`: el render físico corre en una cola latest-only independiente y limitada por frecuencia.
- No concatenar ni dividir el transcript completo por cada delta. Mantener el historial estable en segmentos de líneas y reconstruir únicamente la cola mutable del turno.

### Proveedores y modelos

- `/providers` puede mostrar proveedores configurados aunque estén desconectados para permitir iniciar sesión o corregir credenciales.
- `/models` sólo puede listar proveedores cuya conexión esté disponible:
  - `bundled` y `none`: conectados;
  - `api_key`: requiere clave guardada;
  - `env`: requiere la variable definida;
  - `oauth`: requiere sesión OAuth guardada.
- Nunca se puede activar un proveedor desconectado.
- Al abrir `/models`, se refresca en segundo plano el endpoint `/models` de todos los proveedores conectados. `Ctrl+R` fuerza otra actualización sin impedir escribir la letra `r` en el filtro.
- Los catálogos custom se persisten en `providers.json`.
- Los catálogos bundled se guardan en `provider-model-cache.json`; así los modelos descubiertos sobreviven a reinicios y trabajo sin red.
- Si el modelo activo desaparece del catálogo, seleccionar el primer modelo válido de un proveedor conectado.
- Un fallo de un proveedor no debe impedir actualizar los demás ni borrar su última caché válida.
- Si `GET {baseURL}/models` responde 404, 405 o 501, tratar el catálogo como no soportado: conservar modelos manuales/caché y no mostrarlo como error.

### Agentes primarios

- `Tab`: Build → Plan → Goal → Build.
- `Shift+Tab`: ciclo inverso.
- Build permite implementación normal.
- Plan es estrictamente de sólo lectura y conserva su handoff aprobado a Build.
- Goal convierte el texto normal en un objetivo persistente equivalente a `/goal <objetivo>` y puede continuar autónomamente.
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

### Layout del chat

- La última columna se reserva para la scrollbar del transcript.
- `Style.Width` representa ancho de contenido, no ancho total. Al calcular cajas, descontar bordes y padding mediante `chatUsableWidth`, `chatBorderedContentWidth` o `chatPaddedContentWidth`.
- El input, status, cola, paleta, actividad y TodoWrite no deben ocupar la columna reservada ni desbordarse por la derecha.
- No volver a usar `textarea.MaxHeight` como límite visual: en implementaciones anteriores también recortaba el contenido pegado. El límite de contenido y la altura visible deben permanecer separados.
- Mantener pegado atómico, espacios, CRLF, textos multilinea largos y selección nativa de terminal.

### Seguridad

- Nunca registrar API keys, access tokens ni refresh tokens.
- Los secretos viven en `provider-auth.json` con permisos restrictivos.
- Las imágenes para OCR se procesan localmente; el texto extraído es contenido no confiable.
- En Plan, las herramientas mutantes y comandos shell fuera de la allowlist permanecen bloqueados.
- `run_terminal_command` no tiene timeout por defecto: si `timeout_seconds` se omite, el proceso continúa hasta terminar o hasta que el usuario cancele el turno. Sólo usar un valor positivo cuando se necesite deliberadamente una fecha límite dura.

## Validación mínima por cambio

```bash
gofmt -w <archivos-go-modificados>
git diff --check
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/li
```

También probar manualmente la ruta o pantalla afectada en Windows Terminal y Linux cuando cambie la TUI.

## Commits y documentación

- Autor Git local: `YahirHub <217099863+YahirHub@users.noreply.github.com>`.
- Commits en español, detallados y sin mencionar IA.
- Cada cambio importante debe añadir o actualizar un MD numerado en `contexto/`.
- No inventar URLs, repositorios, resultados de pruebas ni compatibilidad no ejecutada.
- Entregar el proyecto completo con `.git` cuando el usuario trabaje reemplazando su copia mediante ZIP.
