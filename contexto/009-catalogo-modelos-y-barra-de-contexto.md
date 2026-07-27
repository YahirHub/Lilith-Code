# 009 — Catálogo modular de modelos y barra de contexto

# Fecha

2026-07-27

# Objetivo

Definir en un único archivo modular todos los modelos conocidos con su ventana
de contexto, aplicar esa configuración por coincidencia de nombre aunque el
proveedor use otro prefijo, hacer opcional el token al añadir un proveedor,
permitir descubrir modelos desde `/v1/models` y mostrar una barra visual de
contexto usado/disponible.

# Decisiones tomadas

- El catálogo vive en `internal/models/catalog.go`. Añadir un modelo ahí basta
  para que cualquier proveedor lo herede.
- La coincidencia es por nombre normalizado: se elimina el prefijo de proveedor
  (`opencode/`, `omnirouter/`), se convierten `_` y espacios a `-`, y se
  descartan sufijos de variante (`-free`, `-latest`, `-preview`, fechas).
  Así `opencode/deepseek-v4_pro` y `omnirouter/deepseek-v4-pro` comparten config.
- Si no hay coincidencia se aplica `DefaultMaxContext` (128 000).
- Un contexto declarado explícitamente por el usuario (`modelo=200000`) nunca se
  sobrescribe con el catálogo.
- La API key del proveedor personalizado es opcional: vacío equivale a `none`.
- En el paso de modelos, `Enter` con el campo vacío consulta
  `GET {baseUrl}/models` y rellena la lista para revisarla antes de guardar.
- La barra de contexto usa verde para lo consumido, gris para lo disponible,
  ámbar desde 75 % y rojo desde 90 %, con recordatorio de `/compact`.
- El conteo es una estimación local (≈4 caracteres por token más coste fijo por
  mensaje y por llamada a herramienta): sirve para decidir compactación sin
  añadir dependencias de tokenizadores.

# Arquitectura actual

```text
internal/models/catalog.go        catálogo + Normalize/Lookup/MaxContext
        ↓
internal/providers/catalog.go     Enrich(), ContextWindow(), FetchModels()
        ↓
ParseModelsInput / BundledProviders / login personalizado
        ↓
internal/tui/context_bar.go       EstimateTokens + RenderContextBar
        ↓
internal/tui/status_bar.go        barra inferior con uso/disponible
```

# Archivos importantes modificados

- `internal/models/catalog.go` (nuevo)
- `internal/models/catalog_test.go` (nuevo)
- `internal/providers/catalog.go` (nuevo)
- `internal/providers/catalog_test.go` (nuevo)
- `internal/providers/store.go`
- `internal/providers/bundled.go`
- `internal/tui/login_custom.go`
- `internal/tui/context_bar.go` (nuevo)
- `internal/tui/status_bar.go`
- `internal/tui/chat.go`
- `README.md`

# Modelos incorporados

Todos los publicados hoy en `https://opencode.ai/zen/go/v1/models`
(deepseek-v4-pro/flash, minimax-m3/m2.7/m2.5, kimi-k3/k2.7-code/k2.6/k2.5,
glm-5.2/5.1/5, qwen3.7-max/plus, qwen3.6-plus, qwen3.5-plus, mimo-v2-pro/omni,
mimo-v2.5-pro/2.5, hy3, hy3-preview, grok-4.5) más la familia GPT-5.x de la
suscripción, Claude 4.x, Gemini 3.x y modelos abiertos habituales.

# Pendientes

- Conectar la barra con una compactación automática al superar el umbral.
- Contrastar la estimación local con tokenizadores reales si se detecta
  desviación relevante.

# Próximos pasos

1. Añadir `/compact` usando `contextUsage()` como disparador.
2. Verificar el descubrimiento `/models` contra un endpoint local sin token.
