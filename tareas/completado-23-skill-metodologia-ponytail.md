# Tarea 23 — Skill embebida de metodología Ponytail

## Estado

Completada.

## Objetivo

Convertir la metodología universal suministrada por el usuario en una Agent
Skill genérica embebida dentro de Lilith y permitir activar o desactivar skills
individuales desde `/config`, sin eliminar el interruptor global existente.

## Implementación

- Se añadió `assets/skills/ponytail-development/SKILL.md`.
- El cuerpo conserva byte por byte los 26,183 bytes del documento original.
- Se añadió `disabledSkills` a `~/.li/settings.json` como lista negativa.
- Se separó el catálogo crudo del catálogo habilitado en el chat.
- Se creó la sección `/config > Skills` con interruptor maestro y controles por
  skill.
- El filtro individual afecta activación automática, paleta, agentes e
  invocación manual.
- Se actualizaron README, AGENTS y contexto maestro.

## Validación realizada

- `gofmt` sobre todos los archivos Go modificados.
- `git diff --check`.
- `go test ./internal/config ./internal/skills` en una copia temporal con la
  directiva Go ajustada únicamente a la toolchain local 1.23.2.
- `go test -race ./internal/config ./internal/skills`.
- `go vet ./internal/config ./internal/skills`.
- Verificación byte por byte del cuerpo del `SKILL.md` contra el Markdown
  recibido.

## Limitación del entorno

No se pudo compilar localmente `internal/tui` porque el entorno no tiene Go 1.24
ni las dependencias Tview/Tcell/Tree-sitter en caché y no dispone de red para
descargarlas. Las pruebas TUI quedan incluidas para el workflow oficial.
