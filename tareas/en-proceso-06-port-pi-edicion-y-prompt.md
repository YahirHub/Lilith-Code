# Tarea 06 — Portar edición robusta y prompt de pi.dev

## Estado
en-proceso

## Objetivo
Analizar la implementación real de pi.dev proporcionada por el usuario y portar a Lilith las conductas de edición que reducen reintentos y gasto de tokens, manteniendo la política de seguridad de Lilith para `write_file`.

## Alcance
- `str_replace` valida contra el contenido actual del archivo y deja de exigir una llamada ceremonial previa a `read_files`.
- `apply_diff` valida directamente contra el archivo actual y deja de depender de estado `Seen`.
- Compatibilidad con `edits` serializado como JSON string, como tolerancia para modelos que lo emiten incorrectamente.
- Preservar BOM UTF-8 y estilo CRLF/LF al editar.
- Fuzzy matching alineado con pi.dev, incluyendo NFKC.
- `write_file` conserva la regla local de solo crear archivos nuevos y devuelve un resultado recuperable compacto cuando el archivo ya existe.
- Reducir el prompt del sistema y construir instrucciones de herramientas sólo para las herramientas activas, siguiendo el patrón de `promptSnippet` + `promptGuidelines` de pi.dev.
- Añadir pruebas de regresión.

## Referencia analizada
Copia de trabajo local fuera del entregable: `/mnt/data/lab/pi/pi-main`.
