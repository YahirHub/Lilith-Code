# 136. Migración física de slash commands a módulos de feature

## Objetivo

Completar la segunda etapa de la arquitectura introducida en el contexto 135.
La primera etapa creó `internal/moduleapi`, el registry, distribución estática y
módulos iniciales, pero conservaba todos los comandos históricos dentro de
`internal/tui/commands.go` y los envolvía en un mega-módulo `core.commands`.

Eso permitía extensiones privadas, pero dejaba dos arquitecturas: funcionalidades
históricas centralizadas y funcionalidades nuevas modulares. Para que el repo
público use exactamente el mismo patrón que una distribución empresarial, se
elimina esa capa de compatibilidad.

## Estado nuevo

Los slash commands públicos se registran físicamente desde paquetes bajo
`modules/core/**`:

- `core.help` -> `/help`;
- `core.project` -> `/init`;
- `core.goal` -> `/goal`;
- `core.mode` -> `/plan`, `/build`;
- `core.compaction` -> `/compact`;
- `core.rewind` -> `/rewind`;
- `core.fork` -> `/fork`;
- `core.memory` -> `/memory`;
- `core.mcp` -> `/mcp`;
- `core.agents` -> `/tasks`, `/subtask`, `/agents`;
- `core.plugins` -> `/plugins`, `/reload-plugins`;
- `core.providers` -> `/login`, `/providers`, `/models`;
- `core.config` -> `/config`;
- `core.session` -> `/clear`, `/history`, `/exit`;
- `core.shell` -> `/bash`;
- `core.skills` -> `/skills:*`, `/skill:*`;
- `core.modules` -> `/modules`.

`internal/tui/commands.go` ya no contiene implementaciones de comandos ni crea
`core.commands`: únicamente transforma contribuciones del `Registry` a filas de
la paleta/dispatcher TUI.

## Frontera entre módulos y TUI

Los módulos continúan sin importar `internal/tui`. `internal/moduleapi` amplía
sus capacidades opcionales (sin agrandar la interfaz base `Host`) para exponer
operaciones concretas de proyecto, Goal, modos Build/Plan, compactación, forks,
memoria, MCP, agentes, plugins y sesión.

`internal/tui/module_host.go` implementa esas capacidades y traduce las llamadas
a los servicios/UI existentes. Esto permite mover la lógica de comando y su
metadata al módulo sin forzar un refactor destructivo de servicios maduros como
compaction, MCP o session storage.

Se movió al módulo la lógica que no necesita internals de TUI, por ejemplo:

- parsing y comportamiento de `/plan` y `/build`;
- parsing y mensajes de `/memory`;
- formato de `/agents`;
- validación de `/bash`;
- lifecycle simple de `/clear` y `/exit`;
- apertura de screens mediante `ScreenOpener`.

Los servicios de bajo nivel pueden seguir en `internal/**`; ser módulo no implica
mover cada biblioteca o widget a `modules/**`.

## Distribución pública/privada

`internal/distribution/builtin.go` importa exclusivamente `modules/core/**`.
Un repo privado sigue agregando sólo:

```text
modules/company/**
internal/distribution/company.go
```

con build tag `company`. No necesita editar `commands.go`, `chat.go` ni el
selector público, por lo que `merge main` permanece de bajo conflicto.

## Compatibilidad

Se conservan nombres, aliases, descripciones, orden de la paleta y handlers de
comportamiento. Los tests de ownership ahora exigen que cada slash command tenga
un módulo de feature específico y fallan si reaparece `core.commands`.

El contexto 135 describe la etapa inicial; este documento reemplaza únicamente
la parte que mencionaba `core.commands` como estado actual.
