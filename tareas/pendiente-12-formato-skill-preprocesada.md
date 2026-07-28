# Tarea 12 — Evaluar formato de skill preprocesada

## Idea pendiente
Evaluar un formato opcional de skill que ya venga preprocesado por su autor con un índice de búsqueda listo para consumir. Lilith no generaría embeddings ni dependería de `nomic-embed-text` en tiempo de uso: detectaría el artefacto incluido en la propia skill y usaría únicamente una herramienta de búsqueda local sobre ese índice.

## Objetivos a investigar
- Definir un formato portable y reconstruible, sin reemplazar `SKILL.md` ni los recursos originales.
- Decidir si el índice debe ser JSON, SQLite/FTS, binario vectorial u otro formato versionado.
- Permitir que una skill incluya uno o varios índices ya procesados.
- Declarar versión, algoritmo/modelo de embedding si aplica, dimensiones, hashes de archivos y compatibilidad.
- Detectar índices obsoletos cuando cambien los archivos fuente.
- Fallback transparente a las herramientas normales `skill_search`, `skill_files` y `skill_read` cuando no exista índice procesado.
- Mantener las consultas sin LLM generativo y sin generación de embeddings dentro de Lilith.

## Estado
Pendiente de diseño y evaluación; no implementar todavía.
