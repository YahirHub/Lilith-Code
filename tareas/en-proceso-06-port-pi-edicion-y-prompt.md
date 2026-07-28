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

## Ajuste posterior a prueba real
La prueba en Windows confirmó que el guard `FILE_EXISTS` funciona, pero el nombre público `write_file` sigue induciendo a algunos modelos a usarlo para sobrescribir archivos porque esa semántica es común en otros agentes. Se ajustará la superficie pública para exponer `create_file` como herramienta de creación exclusiva, manteniendo compatibilidad interna con `write_file` para sesiones antiguas. El objetivo es prevenir la llamada incorrecta antes del preflight, no sólo recuperarse después.

## Corrección tras segunda prueba real
La captura posterior mostró que `FILE_EXISTS` sí protegía el archivo, pero la protección ocurría demasiado tarde: el modelo ya había generado cientos de líneas dentro de `write_file`. La corrección se amplía así:
- La herramienta pública se renombra a `create_file`; `write_file` queda únicamente como compatibilidad visual para sesiones antiguas y deja de aparecer en el catálogo/esquemas nuevos.
- `create_file` sólo se materializa de entrada cuando el usuario pide explícitamente crear/agregar un archivo; tareas de editar/corregir/refactorizar no reciben esa herramienta automáticamente.
- Mientras los argumentos de `create_file` están llegando por streaming, Lilith hace preflight en cuanto conoce `path`. Si el target ya existe, cancela sólo esa petición SSE antes de que se genere el cuerpo completo, sintetiza `FILE_EXISTS` compacto y continúa el mismo turno con `read_files`, `str_replace` y `apply_diff`.
- Tras cualquier `FILE_EXISTS`, `create_file` se retira de las herramientas activas del resto del turno para impedir reintentos.
- El contenido rechazado se elimina también del panel visible y del historial enviado al proveedor.
