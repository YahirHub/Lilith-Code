# Contexto del proyecto Lilith

> Este documento reúne el estado, decisiones y arquitectura de Lilith
> para que otra IA (o desarrollador humano) pueda continuar el trabajo
> sin necesidad de leer el historial anterior. Escrito el 2026-07-27.

## 1. Qué es Lilith

Lilith (`li`) es un **CLI agéntico estilo Claude/Codex en 100% Go**,
inspirado visual y funcionalmente en `codewolf` (TypeScript + React +
OpenTUI) — el código fuente de referencia vive en `lilith/original/` y
**no se toca**, solo se usa para consultar comportamiento/diseño.

Objetivos:
- Chat interactivo en terminal con Markdown y streaming SSE.
- Múltiples proveedores compatibles con la API de OpenAI
  (OpenAI, Groq, Ollama, LM Studio, OpenRouter, endpoints custom).
- Un proveedor "bundled" gratis (OpenCode Free) que expone modelos
  `-free` obtenidos dinámicamente desde `https://opencode.ai/zen/v1/models`.
- Configuración persistente en `~/.li/` con separación
  ajustes/proveedores/secretos y permisos POSIX estrictos (0700 dir, 0600
  archivos con API keys).
- Onboarding TUI en primera ejecución (3 opciones: Suscripción,
  Proveedor personalizado, OpenCode Free).

## 2. Stack técnico

- Go **1.23+** (`go.mod` declara 1.23; el build local usa 1.25 vía Nix).
- Framework TUI: **Charmbracelet**
  - `bubbletea` — event loop / modelo MVU
  - `bubbles` — `textarea`, `viewport`
  - `lipgloss` — estilos + join layouts
  - `glamour` — render Markdown → ANSI
- CLI framework: `spf13/cobra`
- Sin dependencias fuera de estas + stdlib.

## 3. Estructura de directorios

```
lilith/
├── cmd/li/main.go             # entrypoint cobra; enruta onboarding vs chat
├── internal/
│   ├── config/                # ~/.li/settings.json (ajustes UI, modelo activo)
│   ├── secrets/               # ~/.li/provider-auth.json (API keys, 0600)
│   ├── logx/                  # logger con enmascaramiento de secretos
│   ├── providers/
│   │   ├── types.go           # Provider, ProviderModel, ActiveSelection
│   │   ├── store.go           # carga/guardado ~/.li/providers.json + activo
│   │   ├── upsert.go          # normalización (base URL, IDs, dedupe)
│   │   ├── bundled.go         # OpenCode Free + fetch modelos "-free"
│   │   └── openai/client.go   # cliente OpenAI-compatible con SSE streaming
│   └── tui/
│       ├── app.go             # AppContext (config, providers, cliente, styles)
│       ├── theme.go           # paleta oscura estilo Codewolf (purple)
│       ├── styles.go          # lipgloss styles derivados del theme
│       ├── logo.go            # logo ASCII "LILITH"
│       ├── onboarding.go      # pantalla primera ejecución (3 tarjetas)
│       ├── login_custom.go    # form multi-paso proveedor custom
│       ├── login_codex.go     # login OAuth (placeholder para suscripción)
│       ├── model_selector.go  # selector con fuzzy search y highlight
│       ├── suggestion_menu.go # popup para /comandos
│       ├── status_bar.go      # barra inferior (proveedor · modelo · modo)
│       ├── commands.go        # slash commands: /help /login /models /clear /quit
│       ├── markdown.go        # RenderMarkdown() via glamour (cache por width)
│       ├── thinking.go        # animación shimmer "Pensando..." (spinner + colores)
│       └── chat.go            # ChatModel: transcript + input + streaming
├── original/                  # código fuente de Codewolf (referencia, no editar)
├── Makefile                   # build / test / install
├── go.mod / go.sum
├── contexto/                  # decisiones numeradas (000, 001, ...)
```

## 4. Persistencia (`~/.li/`)

| Archivo              | Permisos | Contenido                                              |
|----------------------|----------|--------------------------------------------------------|
| `settings.json`      | 0600     | `{ activeProviderId, activeModelId, theme, ... }`      |
| `providers.json`     | 0600     | Lista de proveedores custom (id, nombre, baseURL, modelos) |
| `provider-auth.json` | 0600     | `{ [providerId]: apiKey }` — nunca fusionado con providers |

- Directorio creado con `0700`.
- Los proveedores **bundled** (OpenCode Free) no se persisten; se
  reconstruyen en memoria al arrancar y se fusionan con los custom.
- URLs con `http://` solo se aceptan si el host es `localhost` o
  `127.0.0.1` (validación en `providers.NormalizeBaseURL`).

## 5. Proveedores

### 5.1 Interfaz común (`internal/providers/types.go`)
```go
type Provider struct {
    ID       string
    Name     string
    BaseURL  string           // termina en /v1 ya normalizado
    Models   []ProviderModel
    Bundled  bool             // true = catálogo dinámico, sin auth
}
type ProviderModel struct { ID, Name string }
type ActiveSelection struct { ProviderID, ModelID string }
```

### 5.2 OpenCode Free (`internal/providers/bundled.go`)
- `BundledProviders()` intenta `GET https://opencode.ai/zen/v1/models`
  con timeout 3s.
- Filtra los `data[]` cuyo `id` termina en `-free`.
- Si falla la red, cae a una lista hardcodeada (fallback) — a la fecha:
  `deepseek-v4-flash-free`, `mimo-v2.5-free`, `ling-3.0-flash-free`,
  `nemotron-3-ultra-free`, `north-mini-code-free`, `laguna-s-2.1-free`.
- BaseURL fijo: `https://opencode.ai/zen/v1`. No requiere API key.

### 5.3 Cliente OpenAI-compatible (`internal/providers/openai/client.go`)
- Un solo tipo `Client` con método `Stream(ctx, Request) <-chan Chunk`.
- Envía `POST {baseURL}/chat/completions` con `stream: true`.
- Parsea SSE (líneas `data: {...}` + `data: [DONE]`).
- Cierra el canal en `Done` o error. Respeta cancelación de `ctx`.
- `Authorization: Bearer <apiKey>` solo si hay key.

## 6. TUI — pantallas

### 6.1 Flujo global (`cmd/li/main.go`)
```
li → carga config/providers
     ├── si settings.json NO existe → tui.RunFirstRun (onboarding)
     └── si existe                    → tui.RunChat
```

### 6.2 Onboarding (`tui/onboarding.go`)
Tres tarjetas navegables:
1. **Suscripción** → `login_codex.go` (placeholder OAuth, no implementado).
2. **Proveedor personalizado** → `login_custom.go`.
3. **OpenCode Free** → activa bundled y va directo al chat.

### 6.3 Login custom (`tui/login_custom.go`)
Multi-paso: Nombre → Base URL → API Key → descubrir modelos
(`GET /models`) → seleccionar modelo por defecto → guardar y saltar al chat.

### 6.4 Chat (`tui/chat.go`) — pieza principal
- **Header fijo** (`renderHeader`): logo ASCII + tagline + `Directorio ~/ruta`.
- **Transcript** en `viewport.Model`. Cada mensaje:
  - `[HH:MM] tú` / `[HH:MM] ✦ lilith`
  - Contenido indentado 2 espacios.
  - **Asistente**: se pasa por `RenderMarkdown(content, width-2)` (glamour
    con estilo `dark`, wrap por ancho, emoji habilitado).
  - **Mensaje asistente vacío + `thinking`**: se muestra shimmer animado
    en lugar del texto.
- **Input** (`textarea`, 3 filas, `❯ ` prompt), soporte multilínea con
  Shift+Enter, `Enter` envía.
- **Paleta `/comandos`**: se abre cuando el buffer empieza con `/`
  y no tiene espacios ni saltos de línea. Tab autocompleta, Enter ejecuta.
- **Modos**: `default` y `bash` (con prefijo `!`) — bash aún es
  placeholder ("llegará en la próxima fase").
- **Streaming**:
  - `submit()` añade mensaje user + placeholder asistente vacío,
    activa `streaming=true`, `thinking=true`, y despacha
    `tea.Batch(streamPump(ch), thinkingTick(0))`.
  - `chatStreamMsg` con `delta` desactiva `thinking` en el primer chunk
    y agrega texto al `streamBuf`.
  - `chatStreamMsg{done:true}` finaliza y resetea flags.
  - `Esc` cancela el turno activo y restaura la cola al editor. `Ctrl+C` y `Ctrl+Z` no cierran ni suspenden el proceso; la salida explícita es `/exit`.

### 6.5 Animación "Pensando..." (`tui/thinking.go`)
- `thinkingTickMsg` disparado cada **90ms** vía `tea.Tick`.
- `RenderThinking(frame)` produce:
  - Spinner braille (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) con color rotativo.
  - Palabra "Pensando" con **shimmer**: cada letra toma un color de
    la paleta `thinkingPalette` desplazado por `(i+frame)`.
  - Puntos suspensivos que crecen 0→3→0.
- Se muestra únicamente cuando `m.thinking && streamBuf vacío && mensaje
  asistente es el último`.

### 6.6 Markdown (`tui/markdown.go`)
- Wrapper alrededor de `glamour.TermRenderer`.
- Cache del renderer indexado por width (recrea si cambia el ancho).
- Opciones: `WithStandardStyle("dark")`, `WithWordWrap(width)`,
  `WithEmoji()`.
- Recorta saltos de línea sobrantes para no romper el viewport.
- Fallback: si glamour falla, devuelve el texto crudo.

### 6.7 Slash commands (`tui/commands.go`)
Registrados: `/help`, `/login`, `/providers`, `/models`, `/model`,
`/clear`, `/history`, `/config`, `/exit`.
- `/models` abre el `model_selector` (fuzzy search en subsequence
  con resaltado de matches, agrupado por proveedor).
- `/login` reinicia el form custom.
- `/clear` vacía el transcript.

## 7. Convenciones / reglas del proyecto

Metodología "Ponytail" acordada con el usuario:
- **Simplicidad agresiva**: no añadir capas innecesarias.
- **Contexto persistente**: guardar decisiones en `contexto/` cuando el
  proyecto crezca (o en este `contexto.md` mientras sea pequeño).
- **Seguridad primero**: nunca loguear API keys; `logx` enmascara.
- **Sin referencias a IA/marcas** en UI ni commits.
- **Mensajes en español**, sin emojis en commits.
- **Dependencias mínimas**: solo lo estrictamente necesario.

## 8. Estado actual (fase 1 completada)

Funciona:
- [x] Onboarding TUI con 3 opciones.
- [x] Alta de proveedor custom con descubrimiento de modelos.
- [x] OpenCode Free con catálogo dinámico + fallback.
- [x] Chat streaming SSE contra cualquier endpoint OpenAI-compat.
- [x] Slash commands + paleta interactiva.
- [x] **Markdown renderizado en respuestas del asistente** (glamour).
- [x] **Animación shimmer "Pensando..."** mientras espera el primer chunk.
- [x] Cancelación con Esc durante streaming y salida explícita únicamente con `/exit`.
- [x] Tests: `internal/providers` (store, normalización) e
      `internal/tui` (model selector fuzzy).

Pendiente (siguientes fases sugeridas):
- [ ] Ejecución real del modo bash `!comando` (sandbox + captura salida).
- [ ] **Tool calls / function calling** OpenAI (leer archivo, ejecutar
      bash, escribir archivo) con UI colapsable de resultados.
- [ ] Persistencia de sesiones/historial y comando `/sessions`.
- [ ] OAuth real para "Suscripción" (`login_codex.go`).
- [ ] Barra de estado avanzada (tokens usados, latencia, modelo activo
      con tooltip).
- [ ] Manejo de imágenes en el prompt (multipart / data URLs).
- [ ] Empaquetado release (`goreleaser` para linux/mac/win).

## 9. Cómo compilar y probar

```bash
cd lilith
# usa Go 1.23+ del sistema, o vía nix:
nix run nixpkgs#go -- build -o bin/li ./cmd/li
./bin/li            # primera vez: onboarding
./bin/li            # siguientes: chat
```

Tests:
```bash
nix run nixpkgs#go -- test ./...
```

## 10. Referencias clave del código de Codewolf

Los archivos más consultados en `lilith/original/cli/src/` para replicar
comportamiento son:
- `components/first-run-onboarding-screen.tsx` — layout onboarding.
- `components/model-selector-screen.tsx` — fuzzy search agrupado.
- `components/provider-login-screen.tsx` — flujo multi-paso.
- `components/shimmer-text.tsx` — animación shimmer (referencia visual;
  Lilith usa una versión simplificada por letra).
- `components/thinking.tsx` — "Pensando" / spinner.
- `utils/markdown-renderer.tsx` — renderizado Markdown (Codewolf usa
  su propio parser; Lilith delega en glamour, resultado equivalente).
- `providers/opencode-catalog.ts` — endpoint `opencode.ai/zen/v1/models`.

## 11. Cambios recientes (este turno)

1. Añadido `internal/tui/markdown.go` — `RenderMarkdown()` con glamour
   cacheado por ancho, estilo `dark`, wrap y emoji.
2. Añadido `internal/tui/thinking.go` — `RenderThinking(frame)` con
   spinner braille + shimmer por letra + puntos animados; tick 90ms.
3. Modificado `internal/tui/chat.go`:
   - `ChatModel` gana `thinkingFrame` y `thinking bool`.
   - `renderTranscript` renderiza Markdown en asistente y muestra
     shimmer si el mensaje del asistente está vacío y `thinking`.
   - `Update` maneja `thinkingTickMsg` y desactiva `thinking` al primer
     `delta`, al `done` o al `err`.
   - `submit` inicializa el placeholder del asistente como `""` (no `…`),
     activa `thinking` y despacha `tea.Batch(streamPump, thinkingTick(0))`.
4. Añadidas dependencias: `glamour v1.0.0` (+ transitivas: goldmark,
   bluemonday, cellbuf, etc.). Actualizados `lipgloss`, `termenv`,
   `x/ansi`, `runewidth`, `colorful`, `sync`, `sys`, `text`.

Build limpio: `go build ./...` OK. Tests: `go test ./...` OK.
