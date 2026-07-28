# Tarea 11 — Herramientas de búsqueda y lectura acotada de skills

## Objetivo
Crear herramientas nativas de Lilith para descubrir, buscar, listar y leer recursos dentro de una skill sin obligar al modelo a cargar archivos masivos completos en contexto.

## Alcance
- [x] `list_skills`: catálogo compacto de skills disponibles.
- [x] `skill_search`: búsqueda recursiva determinista con ranking por coincidencia, filtros y contexto acotado.
- [x] `skill_files`: listado recursivo de archivos/assets/scripts/references con filtros.
- [x] `skill_read`: lectura paginada de `SKILL.md` o de un archivo concreto dentro de la skill.
- [x] Integrar las herramientas con `tool_search` y con `/skill:<name>`.
- [x] Preferir estas herramientas sobre `read_files` para recursos internos de skills.
- [x] Mantener resultados acotados por resultados, líneas y bytes para reducir tokens.
- [x] No insertar binarios; localizarlos por metadata/path.
- [x] Excluir memoria/documentación de mantenimiento por defecto.
- [x] Añadir pruebas con una skill de ejemplo que contenga references, scripts y assets.
- [x] Reforzar el system prompt para que una skill claramente aplicable sea obligatoria y se cargue `SKILL.md` completo antes de actuar.
- [x] Mantener `skill_read`, `skill_search` y `skill_files` disponibles desde el primer turno sustantivo cuando existan skills visibles.
- [x] Alinear el XML y la invocación explícita con el patrón `name`/`description`/`location` usado por pi.dev.

## Estado
Implementado; pendiente validación completa de la TUI con Go 1.24+ en Windows antes de marcar como completada.

## Validación realizada
- `GO111MODULE=off go test ./internal/skills`: PASS con Go 1.23.2 (paquete stdlib-only).
- Pruebas aisladas de `internal/tools` en laboratorio temporal con imports locales: PASS para las herramientas de skills, activación inicial de la superficie skill y materialización mediante `tool_search`.
- `go vet` de `internal/skills` + `internal/tools` en el mismo laboratorio: PASS.
- El laboratorio/stub no forma parte de la entrega.

## Criterios de finalización
- [x] Herramientas registradas y accesibles de forma lazy.
- [x] `skill_search` devuelve primero los resultados con mayor coincidencia.
- [x] Filtros y límites funcionan.
- [x] No se inyectan binarios ni archivos enormes completos.
- [x] Pruebas de `internal/skills` y pruebas específicas de `internal/tools` pasan en entorno compatible de laboratorio.
- [ ] `go test ./...` y `go vet ./...` confirmados en Windows con Go 1.24+.
