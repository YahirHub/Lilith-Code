# 065 · Optimización de sesiones largas y prompt cache

## Fecha

2026-07-29

## Problema observado

En conversaciones largas Lilith podía tardar progresivamente más entre pulsar Enter y recibir el primer delta del proveedor, incluso usando modelos con ventanas de contexto muy amplias. La causa no era única: había trabajo local O(n) antes de cada request y, además, la forma de reconstruir tools/system prompt reducía la posibilidad de reutilizar prompt caching del proveedor.

## Hallazgos en Lilith

1. `submit()` hacía `persist()` antes de arrancar el request. `persist()` clona todo el historial, serializa todo el transcript y reescribe el JSON completo de la sesión. El coste crecía con cada turno y ocurría justo en la ruta crítica previa al request.
2. La caché de transcript se descartaba cuando `streaming=false`. El siguiente turno volvía a renderizar todo el Markdown histórico con Glamour y podía hacerlo de nuevo al entrar a streaming.
3. `selectToolsForPrompt()` reemplazaba el set activo en cada solicitud. Aunque esto minimiza schemas de una llamada aislada, modifica el prefijo de tools/system prompt de una llamada a la siguiente y perjudica el prompt caching en sesiones largas.
4. TodoWrite y Plan Mode se inyectaban al principio del system prompt. Cada revisión de tareas o cambio de estado alteraba el primer bloque del request, invalidando el prefijo anterior aunque cientos de mensajes históricos fueran iguales.
5. Los resultados antiguos de `read_files`, shell, búsquedas y otras tools, además de argumentos grandes de edición/creación, se reenviaban completos para siempre.
6. `tool_search` materializaba herramientas desde la goroutine de ejecución, mutando `activeTools` fuera del loop de Bubble Tea.

## Referencias estudiadas

### Pi

- Mantiene compaction automática y conserva una cola reciente literal.
- Durante serialización para compaction limita resultados de tools a 2000 caracteres.
- Para dynamic tool loading recomienda mantener el loader activo durante toda la sesión y hacer cambios aditivos; reemplazar tools rompe el prefijo cacheable.
- Advierte que `promptSnippet`/`promptGuidelines` activados dinámicamente también reconstruyen el system prompt y pueden invalidar la caché.

### Claude / Claude Code

Anthropic separa tres problemas:

- compaction para crecimiento general del diálogo;
- tool-result clearing para resultados antiguos que pueden releerse;
- memoria persistente para estado que no necesita estar siempre en contexto.

La documentación también señala que la latencia de prefill crece con la longitud del contexto incluso cuando el modelo todavía está muy lejos de su límite máximo.

### OpenCode

El código y documentación revisados usan:

- compaction con una cola reciente separada;
- resultados de herramientas limitados a 2000 caracteres en el contexto retenido de compaction;
- marcas de cache específicas por proveedor en mensajes estables;
- conservación del historial durable aunque el contexto activo haya sido compactado.

## Cambios implementados

### Persistencia fuera de la ruta crítica

Al comenzar un turno ya no se reescribe la sesión histórica completa salvo en la primera solicitud de una sesión nueva. Se usa el sidecar `.live` existente, que guarda únicamente la cola añadida desde el último snapshot estable.

El snapshot completo sigue escribiéndose al finalizar el turno, por lo que `/history`, `--continue` y la recuperación conservan el comportamiento anterior.

### Caché incremental del transcript entre turnos

El prefijo renderizado ya no se destruye cuando el agente queda idle. Si el historial sólo creció, se renderizan únicamente los mensajes nuevos y se concatenan al prefijo cacheado. Sólo se reconstruye todo cuando cambia el ancho, el prefijo retrocede o una interacción invalida explícitamente el render.

### Tools aditivas por modo

Build y Plan mantienen catálogos de tools materializadas independientes. Una tool descubierta permanece en el set del modo durante la sesión, evitando reemplazar schemas completos en cada prompt. Las prerequisites dinámicas siguen filtrándose justo antes de enviar/ejecutar.

La materialización de `tool_search` ahora se acumula dentro de la goroutine y se aplica al `ChatModel` únicamente al recibir `toolResultsMsg`, evitando mutaciones concurrentes del estado TUI.

### System prompt estable

Los contratos completos continúan en los JSON schemas. El system prompt deja de enumerar `promptSnippet`/`promptGuidelines` según el set exacto de tools; usa un bloque pequeño y estable de reglas generales cuando existe superficie de tools.

TodoWrite y Plan Mode ya no se colocan en el prefijo system del request real. Se agregan solamente a una copia del último mensaje de usuario dentro de `<lilith_runtime_state>`, sin modificar el historial durable. Así una revisión de Todo/Plan no invalida cientos de mensajes anteriores en proveedores con prompt cache por prefijo.

### Clearing local de resultados antiguos

El historial durable y visual permanece completo. Sólo la copia enviada al proveedor se compacta:

- los dos turnos de usuario más recientes permanecen literales;
- resultados de tools más antiguos se limitan a aproximadamente 2000 caracteres, conservando principio y final;
- argumentos antiguos muy grandes (`content`, `old`, `new`, `patch`, etc.) se sustituyen por una representación compacta y válida;
- `skill_read`, `todo_write`, `plan_question` y `plan_exit` quedan excluidos del clearing automático.

Si el modelo vuelve a necesitar contenido exacto de un resultado eliminado puede ejecutar nuevamente la herramienta correspondiente.

## Qué no se hizo todavía

Este cambio no implementa todavía una compaction LLM completa tipo Pi/OpenCode. El objetivo inmediato es corregir la degradación temprana que aparecía incluso con una ventana de 1M casi vacía. Una compaction automática sigue siendo útil cuando el diálogo normal (no sólo tool outputs) llegue a ser grande, pero debe implementarse como checkpoint persistente y no como borrado del historial visible.

## Validación disponible

- `gofmt` aplicado.
- `git diff --check` limpio.
- Los 118 archivos Go del proyecto pasan `go/parser` sin errores sintácticos.
- Se añadieron regresiones para clearing de tool outputs, compactación de argumentos y colocación del runtime state en la cola del prompt.
- La suite completa no puede ejecutarse en el sandbox porque el proyecto exige Go 1.24 y las dependencias Charm/x/text no están disponibles localmente ni puede resolverse `proxy.golang.org`.
