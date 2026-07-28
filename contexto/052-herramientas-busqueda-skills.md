# Fecha
2026-07-28

# Objetivo
Evitar que Lilith tenga que leer archivos grandes completos dentro de una skill. Añadir navegación nativa, local y acotada para descubrir skills, buscar coincidencias relevantes, listar recursos y leer únicamente rangos concretos.

# Decisiones tomadas
- Mantener `SKILL.md` como punto de entrada de la skill y conservar la carga explícita con `/skill:<nombre>` / `/skills:<nombre>`.
- Añadir herramientas nativas de sólo lectura: `list_skills`, `skill_search`, `skill_files` y `skill_read`.
- `skill_search` usa ranking lexical determinista local. No usa LLM, embeddings, procesos externos ni scripts incluidos en la skill.
- La búsqueda puede filtrar por subruta, patrón, tipo de recurso, extensión, regex, sensibilidad a mayúsculas, cantidad de resultados y líneas de contexto.
- Los resultados se ordenan por score de coincidencia y tienen límites de resultados/bytes para no llenar el contexto.
- Los assets binarios no se insertan en contexto: pueden localizarse por nombre/path y `skill_read` sólo devuelve metadata.
- `AGENTS.md`, `README.md`, `CLAUDE.md` y tutoriales de mantenimiento se excluyen de la búsqueda/listado runtime por defecto; pueden incluirse explícitamente con `include_maintenance`.
- Las herramientas operan sólo sobre skills previamente descubiertas. No aceptan una ruta arbitraria como raíz.
- Se amplió el descubrimiento para soportar rutas Lilith, Claude y Agent tanto de usuario como de proyecto, con precedencia proyecto > usuario y `.li` sobre rutas compatibles dentro del mismo ámbito.
- El parser liviano de frontmatter soporta `description: >` / `|`, evitando una dependencia YAML adicional.
- La idea de una skill que ya incluya un índice preprocesado se documentó aparte como tarea pendiente 12. No se implementan embeddings ni indexación de ese formato en este cambio.

# Arquitectura actual
```text
skills disponibles
    ↓ metadata SKILL.md
<available_skills> en system prompt
    ↓
tool_search (lazy)
    ↓
┌─────────────┬──────────────┬─────────────┬────────────┐
│ list_skills │ skill_search │ skill_files │ skill_read │
└─────────────┴──────────────┴─────────────┴────────────┘
                       ↓
              raíz validada de skill
                       ↓
        búsqueda/listado/lectura local acotada
```

`/skill:<nombre>` sigue cargando el `SKILL.md` principal de forma explícita, pero activa también las cuatro herramientas para que `references/`, `assets/`, `scripts/` y `examples/` se consulten progresivamente.

# Librerías usadas
- Sólo librería estándar de Go para discovery, walking, regex, lectura y ranking.
- No se añadió SQLite, FTS, embeddings ni dependencias nuevas.

# Archivos importantes modificados
- `internal/skills/skills.go`
- `internal/skills/resources.go`
- `internal/skills/skills_test.go`
- `internal/skills/resources_test.go`
- `internal/tools/registry.go`
- `internal/tools/skills.go`
- `internal/tools/skill_tools_test.go`
- `internal/tui/chat.go`
- `internal/tui/config_screen.go`
- `tareas/en-proceso-11-herramientas-busqueda-skills.md`
- `tareas/pendiente-12-formato-skill-preprocesada.md`

# Problemas encontrados
- El loader anterior sólo inspeccionaba `~/.li/skills` y `./.li/skills` un nivel por debajo.
- El parser de frontmatter sólo manejaba `key: value`; skills con `description: >` podían quedar con metadata incorrecta.
- Leer directamente archivos completos de `references/`, scripts o templates grandes puede desperdiciar muchos tokens.
- `AGENTS.md` de algunas skills contiene memoria extensa y duplicada del mantenedor; buscarlo por defecto puede desplazar resultados runtime realmente útiles.
- Los assets binarios necesitan ser descubribles sin enviar sus bytes al modelo.

# Soluciones implementadas
- Discovery recursivo y compatible con rutas `.li`, `.claude` y `.agents`.
- Ranking por frase completa, cobertura de términos, repeticiones, nombre/path y headings.
- Filtros estructurados y resultados con contexto limitado.
- Límites de archivos escaneados, cantidad de hits, líneas, longitud por línea y bytes de salida.
- Lectura por `offset`/`limit` con protección contra path traversal.
- Clasificación de recursos: instructions, reference, script, asset, example, test y other.
- Integración lazy con `tool_search` y catálogo de skills inyectado en `tools.Env`.
- `/config` muestra discovery compatible en lugar de afirmar que sólo existen rutas `.li`.

# Pendientes
- Validar la TUI completa con Go 1.24+ en Windows mediante `go test ./...` y `go vet ./...`.
- Evaluar el formato opcional de skill preprocesada documentado en `tareas/pendiente-12-formato-skill-preprocesada.md`.
- No implementar dicho formato hasta definir contrato/versionado y compatibilidad.

# Próximos pasos
Probar con una skill grande real que contenga `references/`, `scripts/` y muchos `assets/`: invocarla, buscar componentes concretos con `skill_search`, filtrar assets con `skill_files` y confirmar que sólo se leen rangos necesarios con `skill_read`.
