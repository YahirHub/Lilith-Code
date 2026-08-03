# 106 · Skill embebida de metodología Ponytail y control individual

# Fecha

2026-08-03

# Objetivo

Convertir la metodología universal suministrada por el usuario en una capacidad
nativa y reutilizable de Lilith, preservando el documento completo y permitiendo
activar o desactivar cada skill sin depender únicamente del interruptor global.

# Decisiones tomadas

- Reutilizar la infraestructura existente de Agent Skills embebidas; no crear un
  segundo sistema de prompts ni una capa de “habilidades internas” paralela.
- Añadir `ponytail-development` como skill genérica dentro de
  `assets/skills/ponytail-development/SKILL.md`.
- Conservar literalmente las 27 secciones de la metodología y añadir sólo el
  frontmatter necesario para descubrimiento, invocación manual y modelo heredado.
- Mantener `SkillsEnabled` como interruptor maestro y añadir `DisabledSkills`
  como lista de excepciones individuales por nombre.
- Habilitar por defecto cualquier skill que no aparezca en esa lista, incluyendo
  skills nuevas de futuras versiones.
- Añadir una sección independiente `Skills` en `/config` para evitar saturar la
  página General y permitir navegar, activar o desactivar cada entrada.
- Aplicar el filtro individual después de resolver precedencia y overrides, para
  que afecte a la skill efectiva que gane por nombre.

# Arquitectura actual

```text
assets/skills/ponytail-development/SKILL.md
        ↓ go:embed
~/.li/.cache/bundled-skills/<hash>/
        ↓ loader común + precedencia
catálogo completo de skills
        ↓ Claude overrides
DisabledSkills de ~/.li/settings.json
        ↓
activación automática / paleta / agentes / invocación manual
```

`/config > Skills` usa el catálogo sin filtrar para que una skill desactivada
continúe visible y pueda reactivarse. El chat usa el catálogo filtrado.

# Librerías usadas

No se agregaron dependencias. Se reutilizan:

- `embed` y `io/fs` de la librería estándar;
- runtime existente de `internal/skills`;
- configuración JSON y escritura atómica de `internal/config`;
- componentes Tview/Tcell propios de `/config`.

# Archivos importantes modificados

- `assets/skills/ponytail-development/SKILL.md`
- `assets/skills/README.md`
- `internal/config/config.go`
- `internal/config/skills_test.go`
- `internal/skills/bundled_test.go`
- `internal/tui/chat.go`
- `internal/tui/config_screen.go`
- `internal/tui/config_screen_test.go`
- `internal/tui/skill_preferences_test.go`
- `README.md`
- `AGENTS.md`
- `tareas/completado-23-skill-metodologia-ponytail.md`

# Problemas encontrados

- Lilith ya tenía skills embebidas y un interruptor global, pero no control
  individual propio.
- La página General había dejado de mostrar nombres de skills para no saturarse;
  volver a poner una lista allí habría reintroducido ese problema.
- Una lista de skills habilitadas habría desactivado silenciosamente skills nuevas
  después de una actualización.

# Soluciones implementadas

- Se creó una sección dedicada de Skills con viewport y navegación existentes.
- Se persiste sólo la lista negativa `disabledSkills`, normalizada y ordenada.
- El catálogo se separó en `loadSkillsCatalog` y `loadSkills`: el primero permite
  administración; el segundo aplica preferencias antes de exponer skills al
  modelo y a la interfaz de invocación.
- La invocación manual ahora distingue una skill inexistente de una skill
  desactivada y dirige al usuario a `/config > Skills`.
- Se añadieron pruebas de materialización, normalización, persistencia, UI y
  filtrado del catálogo.

# Pendientes

- Validar manualmente la nueva sección en Windows Terminal y Linux con una
  terminal estrecha y varias skills externas instaladas.
- Ejecutar la suite completa con Go 1.24 y dependencias descargadas en CI.

# Próximos pasos

1. Abrir `/config`, entrar en `Skills` y activar el interruptor maestro.
2. Confirmar que `ponytail-development` aparece como origen interno.
3. Desactivarla y comprobar que desaparece de `/skill:` y de la activación
   automática sin afectar otras skills.
4. Reactivarla e invocar `/skill:ponytail-development <tarea>`.
