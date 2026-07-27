# Fecha

2026-07-27

# Objetivo

Incorporar el código fuente original de Codewolf (TypeScript + React +
OpenTUI) dentro del repositorio de Lilith como **referencia de portado**,
y reorganizar la documentación de contexto en la carpeta `contexto/`
numerada según la metodología del proyecto.

# Decisiones tomadas

1. El original vive en `lilith-f3/original/` y es **solo lectura**. No se
   edita, no se compila, no se enlaza. Sirve para consultar comportamiento,
   layout, nombres de pantallas y contratos de datos al portar a Go.
2. `contexto.md` (archivo suelto) se movió a
   `contexto/000-contexto-maestro.md`. A partir de ahora cada cambio
   relevante crea un archivo numerado nuevo en `contexto/`.
3. Se copió el árbol completo **excluyendo metadatos de Git** para no
   contaminar el historial del repositorio de Lilith.
4. No se añadió `original/` a `.gitignore`: se versiona a propósito para
   que cualquier persona que continúe el portado tenga la referencia.

# Arquitectura actual

Sin cambios en el código Go. La estructura del repositorio queda:

```text
lilith-f3/
├── cmd/li/main.go
├── internal/{config,secrets,logx,providers,tui}
├── original/            # Codewolf TS/React (referencia, NO editar)
├── contexto/
│   ├── 000-contexto-maestro.md
│   └── 001-referencia-original-codewolf.md
├── Makefile
├── go.mod / go.sum
└── README.md
```

# Librerías usadas

Ninguna nueva. `go.mod` intacto: bubbletea, bubbles, lipgloss, glamour,
cobra y transitivas.

El original trae su propio ecosistema (Bun, React, OpenTUI, zod...) que
**no** se instala ni se ejecuta: es material de lectura.

# Archivos importantes modificados

- `contexto.md` → renombrado a `contexto/000-contexto-maestro.md`.
- `contexto/001-referencia-original-codewolf.md` — este archivo.
- `original/**` — 1.123 archivos, 6.8 MB, añadidos como referencia.

# Mapa de referencia para el portado

Correspondencia entre el original y los paquetes Go de Lilith:

| Original (`original/cli/src/`)                     | Destino en Lilith                      | Estado    |
|----------------------------------------------------|----------------------------------------|-----------|
| `components/first-run-onboarding-screen.tsx`        | `internal/tui/onboarding.go`           | portado   |
| `components/provider-login-screen.tsx`              | `internal/tui/login_custom.go`         | portado   |
| `components/model-selector-screen.tsx`              | `internal/tui/model_selector.go`       | portado   |
| `components/chatgpt-codex-login-screen.tsx`         | `internal/tui/login_codex.go`          | pendiente |
| `components/provider-manager-screen.tsx`            | sin equivalente                        | pendiente |
| `components/chat-history-screen.tsx`                | sin equivalente (`/sessions`)          | pendiente |
| `components/rewind-screen.tsx`                      | sin equivalente (modo plan / rewind)   | pendiente |
| `components/pending-bash-message.tsx`               | modo bash `!comando`                   | pendiente |
| `providers/opencode-catalog.ts`                     | `internal/providers/bundled.go`        | portado   |
| `providers/model-catalog.ts`                        | metadata de modelos                    | parcial   |
| `providers/nvidia-nim-catalog.ts`                   | sin equivalente                        | pendiente |
| `utils/custom-providers.ts`                         | `internal/providers/{store,upsert}.go` | portado   |
| `utils/config-dir.ts`                               | `internal/config`                      | portado   |
| `utils/chatgpt-oauth.ts`                            | OAuth de suscripción                   | pendiente |
| `utils/context-window.ts`, `context-report.ts`      | medidor de contexto                    | pendiente |
| `agents/**`                                         | tool calls / subagentes                | pendiente |

Documentación del original útil para el portado:
`original/docs/{custom-providers,chat-sessions,safe-mode,token-usage,agents-and-tools}.md`
y su propio `original/contexto/` con 70 archivos de decisiones históricas.

# Problemas encontrados

- El ZIP del original no incluye `.git`, así que no hay historial ni tags
  de referencia; solo el árbol de archivos.
- Hay dos catálogos de OpenCode en el original (`free` y `go`); Lilith
  solo implementa `free`. La lista de modelos fallback difiere: el
  original ya usa `hy3-free` donde Lilith tiene `ling-3.0-flash-free` y
  `laguna-s-2.1-free`.
- El original cachea el catálogo de modelos en disco
  (`opencode-models.json`); Lilith hoy solo tiene fallback en memoria.

# Soluciones implementadas

Ninguna todavía: este turno es únicamente incorporación de la referencia
y reorganización del contexto. Los desajustes quedan registrados arriba
para atacarlos en la fase correspondiente.

# Pendientes

- [ ] Alinear la lista de modelos fallback de `bundled.go` con
      `original/cli/src/providers/model-catalog.ts`.
- [ ] Decidir si Lilith cachea el catálogo en `~/.li/opencode-models.json`.
- [ ] Ejecución real del modo bash `!comando`.
- [ ] Tool calls / function calling.
- [ ] Persistencia de sesiones e historial (`/sessions`).
- [ ] OAuth real de suscripción.
- [ ] Empaquetado de releases multiplataforma.

# Próximos pasos

Elegir la siguiente fase de portado. Recomendación por valor/riesgo:
**tool calls + modo bash real**, porque comparten la capa de ejecución,
confirmación del usuario y renderizado colapsable de resultados.
Antes de escribir código hay que definir la política de seguridad:
confirmación explícita por comando, límite de tamaño de salida, timeout
y directorio de trabajo acotado al proyecto.