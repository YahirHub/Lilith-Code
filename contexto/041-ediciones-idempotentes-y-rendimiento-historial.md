> **Actualización 2026-07-27:** la obligación de `read_files`/`Env.Seen` antes de editar quedó reemplazada por la arquitectura documentada en `045-port-pi-edicion-y-prompt.md`. Las herramientas de edición validan el contenido actual en disco en el momento de ejecutar.

# 041 — Ediciones idempotentes y rendimiento del historial TUI

# Fecha

2026-07-27

# Objetivo

Evitar que una edición múltiple falle por incluir un reemplazo que ya está aplicado y
reducir el lag que aparecía conforme aumentaba el historial visible, conservando el
transcript completo y el scroll manual hacia mensajes anteriores.

# Decisiones tomadas

- Un par `old == new` en `str_replace` es idempotente y se ignora. No debe abortar los demás reemplazos del lote.
- Si todos los pares son no-ops, la herramienta responde éxito sin tocar el archivo.
- `write_file` sólo crea archivos nuevos. Un archivo existente debe modificarse mediante `str_replace` o `apply_diff`; esto evita que un fallo quirúrgico derive en una reescritura completa accidental.
- Cuando la TUI proporciona seguimiento de archivos vistos, `str_replace` y `apply_diff` requieren que el archivo haya sido leído durante la sesión actual.
- No se recorta el historial para acelerar la interfaz. El `viewport` sigue conteniendo todas las líneas y el usuario puede subir con teclado o rueda.
- El cuello de botella principal era reconstruir repetidamente todo el transcript: la versión de Bubbles usada por el proyecto implementa `SetContent` normalizando y separando todo el string en líneas. Por ello se redujo la frecuencia de `SetContent` y el trabajo previo de renderizado.
- El shimmer fijo no forma parte del viewport, así que sus ticks sólo necesitan cambiar el frame y dejar que Bubble Tea ejecute `View`; no necesitan regenerar mensajes antiguos.
- Durante streaming se conserva un prefijo renderizado hasta el último mensaje del usuario y sólo se vuelve a renderizar la cola dinámica del turno actual.
- Los repintados provocados por deltas se agrupan en ventanas de 50 ms. Cuando el usuario está desplazado hacia mensajes antiguos se amplía a 250 ms para que el scroll tenga prioridad. El contenido no se descarta: `streamBuf`, mensajes y tool calls continúan acumulándose y el siguiente repintado muestra el estado más reciente.
- La estimación de contexto se cachea porque antes recorría todo `history` y consultaba skills/configuración en cada `View`, incluyendo cada frame del shimmer.
- El elapsed de comandos se actualiza una vez por segundo, coherente con el formato visible en segundos enteros.
- No se activó el antiguo modo `HighPerformanceRendering`: además de requerir manejo especial de scroll-area, las versiones recientes de Bubbles lo consideran obsoleto. La optimización se mantiene sobre el viewport normal y evita una migración mayor de dependencias.

# Arquitectura actual

```text
mensajes completos en memoria
        |
        +--> viewport conserva transcript completo -> PgUp/rueda siguen disponibles
        |
        +--> turno activo
              |-- prefijo estable renderizado una vez
              |-- cola dinámica renderizada al cambiar
              `-- SetContent agrupado cada ~50 ms como máximo

thinkingTick (~90 ms)
        `--> sólo actualiza frame del cromo fijo; NO reconstruye transcript

View()
        `--> contextUsage cacheado; O(1) mientras historial/modelo no cambien
```

# Librerías usadas

No se añadieron ni actualizaron dependencias. Se mantienen Bubble Tea, Bubbles,
Lip Gloss y Glamour ya presentes en el proyecto.

# Archivos importantes modificados

- `internal/tools/files.go`
- `internal/tools/diff.go`
- `internal/tools/registry.go`
- `internal/tools/tools_test.go`
- `internal/tui/chat.go`
- `internal/tui/chat_layout_test.go`
- `internal/tui/app.go`
- `tareas/en-proceso-02-str-replace-y-rendimiento-historial.md`

# Problemas encontrados

1. `applyEdits` devolvía error inmediatamente ante cualquier `old == new`. En un lote de cinco cambios, un quinto cambio ya aplicado anulaba también los cuatro cambios válidos.
2. Tras ese error, el modelo podía intentar recuperarse con `write_file`. La herramienta permitía sobrescribir un archivo existente si antes había sido leído, pese a que su descripción decía que debía usarse para archivos nuevos.
3. Cada tick de `Pensando/Trabajando` llamaba a `refreshTranscript`, aunque el shimmer se dibuja fuera del transcript.
4. Cada delta de streaming podía ejecutar `RenderMarkdown` sobre respuestas antiguas, envolver todo el transcript y llamar a `viewport.SetContent`.
5. `contextUsage()` se ejecutaba desde `View()` y recorría todo el historial en cada frame; además reconstruía el system prompt y consultaba la configuración de skills.
6. El panel de comandos refrescaba dos veces por segundo aunque el tiempo mostrado se redondea a segundos enteros.

# Soluciones implementadas

- No-ops idempotentes en `str_replace` se saltan sin error.
- `write_file` rechaza cualquier ruta de archivo ya existente.
- Lectura previa obligatoria para ediciones cuando `Env.Seen` está habilitado.
- Prompt de herramientas ajustado para re-leer y reintentar cambios mínimos tras fallos.
- Caché temporal del prefijo estable del transcript durante streaming.
- Throttle de repintado del viewport a 50 ms para ráfagas de SSE y 250 ms mientras el usuario está leyendo historial antiguo.
- El tick del shimmer ya no llama a `SetContent`.
- Caché de uso de contexto invalidada al cambiar historial o al volver al chat desde una pantalla que pudo modificar proveedor/configuración.
- Tick de elapsed reducido a 1 segundo.
- Pruebas de scroll verifican que añadir contenido mientras `userScrolled=true` conserva el `YOffset` y aumenta el total de líneas en vez de truncarlo.

# Pendientes

- Ejecutar `go test ./...` en el entorno local con Go 1.24+ y dependencias disponibles.
- Validar manualmente una sesión realmente larga en Windows Terminal/PowerShell y confirmar la mejora perceptible mientras llegan tool calls grandes.
- La tarea queda `en-proceso` hasta esa validación local; no se pierde ni compacta el historial visible como parte de esta optimización.

# Próximos pasos

1. Ejecutar la suite completa.
2. Reanudar una sesión grande y probar PgUp, PgDown, rueda, Home/End durante streaming.
3. Repetir una edición múltiple que incluya deliberadamente un par `old == new` y confirmar que los demás cambios sí se aplican.
4. Si todavía hubiera lag extremo con historiales de decenas de miles de líneas, medir antes de considerar una actualización mayor de Bubbles.
