# Fecha
2026-07-28

# Objetivo
Mejorar la activación automática de Agent Skills para que Lilith no ignore una skill cuando la descripción coincide claramente con la tarea del usuario.

# Decisiones tomadas
- Adoptar el patrón de prompt de pi.dev: el system prompt siempre lista `name`, `description` y `location` de cada skill visible y exige cargar `SKILL.md` cuando la tarea coincide.
- Reforzar la regla respecto a pi.dev: en Lilith una skill claramente aplicable es obligatoria, no una sugerencia. El modelo debe cargar su `SKILL.md` antes de inspeccionar el proyecto, editar, ejecutar comandos o responder de forma sustantiva.
- `skill_read` es la herramienta de bootstrap: para una skill aplicable debe leerse primero `SKILL.md`. El `SKILL.md` usa un lote inicial amplio (hasta 500 líneas bajo el límite de bytes) y, si queda paginado, el prompt obliga a continuar hasta cargarlo completo. `skill_search` y `skill_files` quedan para referencias/assets/scripts secundarios grandes.
- Mantener `skill_read`, `skill_search` y `skill_files` activas desde el inicio de cualquier turno sustantivo cuando existen skills visibles. Esto evita depender de una llamada extra a `tool_search` sólo para poder cargar la skill.
- Los saludos/acks puros siguen sin cargar schemas de tools para no desperdiciar tokens.
- La invocación explícita `/skill:<nombre>` / `/skills:<nombre>` usa ahora un bloque `<skill name="..." location="...">` con la base de rutas relativa, equivalente al contrato estructural de pi.dev.

# Arquitectura actual
```text
mensaje sustantivo del usuario
        ↓
skills habilitadas + catálogo no vacío
        ↓
activar skill_read + skill_search + skill_files
        ↓
system prompt
  <available_skills>
    name + description + location
  </available_skills>
        ↓
¿la tarea coincide claramente?
        ├─ no → continuar normalmente
        └─ sí → MUST skill_read(SKILL.md)
                    ↓
               seguir instrucciones
                    ↓
       references/assets/scripts grandes
          skill_search / skill_files /
             skill_read acotado
```

# Librerías usadas
- Sólo librería estándar de Go.
- No se agregaron dependencias.

# Archivos importantes modificados
- `internal/skills/skills.go`
- `internal/skills/skills_test.go`
- `internal/tools/registry.go`
- `internal/tools/skills.go`
- `internal/tools/skill_tools_test.go`
- `internal/tui/chat.go`
- `contexto/053-activacion-confiable-skills.md`
- `tareas/en-proceso-11-herramientas-busqueda-skills.md`

# Problemas encontrados
- El prompt anterior decía `prefer` y sugería empezar por `skill_search`; eso convertía una skill aplicable en una recomendación opcional y permitía saltarse `SKILL.md`.
- Lilith sólo tenía `tool_search` siempre activo. Aunque el prompt listara skills, `skill_read` podía no estar presente en los schemas del primer request, obligando al modelo a descubrir la herramienta antes de cargar la skill.
- Pi.dev puede usar su herramienta genérica `read`, que ya está disponible para cargar `SKILL.md`; Lilith usa herramientas dedicadas y necesitaba asegurar esa superficie explícitamente.

# Soluciones implementadas
- Prompt obligatorio de activación por coincidencia de descripción.
- XML compatible con `<location>`.
- Regla explícita de no afirmar que se usa una skill sin haber cargado `SKILL.md`.
- Carga de múltiples skills cuando varias descripciones sean claramente necesarias.
- Herramientas de navegación de skills disponibles desde el primer request sustantivo si hay skills visibles.
- `skill_search` ya no se presenta como sustituto de las instrucciones principales de `SKILL.md`.
- Invocación explícita estructurada al estilo pi.dev.

# Pendientes
- Validar `go test ./...` y `go vet ./...` en Windows con Go 1.24+.
- Probar con varias skills reales simultáneamente para comprobar que el modelo carga únicamente las que coinciden y no todas indiscriminadamente.
- Evaluar más adelante si hace falta un matcher determinista previo a la llamada al modelo; de momento no se añade para evitar falsos positivos y mantener el diseño simple.

# Próximos pasos
Probar tareas que coincidan claramente con una skill sin nombrarla explícitamente, observar que la primera acción relevante sea `skill_read` sobre `SKILL.md`, y verificar después la navegación selectiva de assets/references mediante `skill_search`.
