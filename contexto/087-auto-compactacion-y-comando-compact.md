# 087 — Auto compactación de contexto y comando `/compact`

## Objetivo

Evitar que conversaciones largas fallen o se vuelvan inutilizables al acercarse a la ventana máxima del modelo, manteniendo suficiente continuidad técnica para seguir trabajando y sin borrar el historial visible ni los mensajes originales persistidos.

## Referencias de comportamiento

Se revisaron las implementaciones públicas de Pi y OpenCode:

- Pi dispara la compactación antes del límite, reserva espacio para la respuesta, conserva una cola reciente y permite `/compact [instructions]`.
- OpenCode conserva fronteras de turnos recientes, trata las compactaciones anteriores como resúmenes iterativos y evita que grandes salidas históricas de herramientas dominen el resumen.

Lilith adopta esos principios dentro de su arquitectura Go/Tview y su formato de sesión existente.

## Arquitectura

### `internal/compaction`

Paquete independiente de la TUI que contiene:

- estimación determinista de mensajes y schemas de herramientas;
- cálculo del umbral automático;
- selección de prefijo y cola reciente;
- reconocimiento de un resumen anterior;
- serialización acotada de tool calls, resultados y reasoning;
- prompt de handoff estructurado;
- reconstrucción `resumen + tail exacto`.

La cola comienza normalmente en un mensaje de usuario. El objetivo reciente es 20,000 tokens, pero se adapta a `contextWindow/4` en modelos pequeños (mínimo 2,000) para que una ventana de 8k/16k/32k sí pueda liberar espacio. Se preservan dos turnos completos cuando caben. Si el request cruza el umbral por el system prompt o los schemas aunque todo el historial quepa en esa cola, se activa un fallback que resume todos los turnos anteriores y conserva exacto el turno de usuario más reciente. Un único turno autónomo enorme puede cortarse en un mensaje assistant; nunca se corta en un resultado `tool`. Si ni siquiera una frontera segura cabe, se resume el contexto activo completo y todos los mensajes originales quedan archivados. El corte se calcula sobre la copia de request que ya poda salidas históricas, pero el resumen y el archivo reciben el contenido original exacto.

### Orquestación TUI

`internal/tui/compaction.go` ejecuta el resumen como una petición independiente al proveedor/modelo activo, sin schemas de herramientas. Solicita streaming para máxima compatibilidad y acumula el resultado internamente; los proveedores marcados como no-streaming siguen usando su transporte normal.

Casos soportados:

1. **Automático:** antes de cada request normal se calcula el tamaño del contexto preparado más los schemas de herramientas. Si alcanza `contextWindow - 16,384`, se compacta y luego se reanuda el mismo turno. En ventanas menores que la reserva se usa un umbral proporcional válido. Después de un resumen exitoso se vuelve a evaluar el request: una sobrecarga grande de instrucciones/schemas puede requerir una segunda reducción segura de dos turnos exactos a uno.
2. **Manual:** `/compact` o `/compact <instrucciones>` fuerza la compactación cuando el agente está en reposo, aunque el historial todavía quepa en la cola normal. Conserva exacto el turno más reciente y las instrucciones sólo enfocan el resumen.
3. **Recuperación:** si un proveedor devuelve un error reconocible de ventana/contexto, se cancela ese request, se compacta y se reintenta el mismo turno.
4. **Cancelación:** Esc cancela una compactación manual; cancelar el turno cancela también su compactación automática.

Un guard por longitud de historial impide repetir indefinidamente una compactación fallida o intentar compactar otra vez cuando ya no existe un turno anterior reducible. Una compactación exitosa limpia el guard para permitir la reevaluación inmediata del request reconstruido.

## Persistencia

`Session.Compactions` guarda por operación:

- identificador y fecha;
- si fue automática o manual;
- instrucciones opcionales;
- resumen producido;
- tokens estimados antes/después;
- copia exacta de los mensajes retirados del contexto activo.

El historial enviado al proveedor se reduce, pero el transcript renderizado no cambia. `/history` cuenta mensajes de usuario activos, archivados y del checkpoint live, ignorando los mensajes internos `<conversation_summary>`.

El título de una sesión tampoco puede derivarse del marcador de resumen.

## Formato de resumen

El modelo debe devolver sólo un handoff Markdown con:

- objetivo;
- instrucciones del usuario;
- decisiones y enfoques rechazados;
- trabajo completado;
- archivos y símbolos;
- estado actual;
- pendientes y riesgos;
- detalles exactos necesarios para continuar.

Las salidas históricas muy grandes de herramientas se acotan para que la solicitud de resumen quepa en la ventana. También existe un límite global para historiales patológicos con miles de mensajes pequeños. El resumen previo se presenta por separado y se actualiza de forma iterativa. El contenido histórico se marca como datos no confiables: se conserva como hecho, pero no se ejecutan instrucciones incrustadas durante la compactación.

## Invariantes

- La compactación no debe borrar ni reescribir el transcript visible.
- Los mensajes originales retirados deben quedar archivados.
- Nunca enviar herramientas en la petición de resumen.
- No cambiar el modo Build/Plan/Goal de un turno activo: la estimación usa `turnAgentMode`.
- Una respuesta tardía de una compactación cancelada se ignora mediante ID. Mensajes anexados después del snapshot de compactación se concatenan al contexto reconstruido y nunca se pierden.
- Un error de compactación automática no debe perder el turno; se intenta continuar con el historial existente.
- Un overflow sólo se reintenta cuando existe un corte útil. Si el request reconstruido sigue excedido por overhead estático, puede hacerse una segunda compactación; se detiene cuando sólo queda el turno más reciente exacto.

## Pruebas

Se cubren:

- corte normal en fronteras completas de turnos;
- split-turn seguro para un turno único enorme y fallback de resumen completo;
- adaptación del tail a ventanas pequeñas;
- conservación mínima de turnos recientes;
- resumen iterativo;
- reconstrucción del contexto;
- umbral automático, incluyendo schemas y fallback por overhead no histórico;
- acotación de payloads históricos;
- persistencia del archivo de compactación;
- conteo de turnos archivados/live;
- detección de errores de overflow;
- registro del comando `/compact`;
- preservación del transcript y de mensajes anexados durante una compactación.

## Prueba manual recomendada

1. Abrir una sesión larga con un modelo cuyo contexto esté correctamente declarado.
2. Continuar hasta cruzar el umbral y comprobar el indicador `Compactando…`.
3. Verificar que el turno se reanuda automáticamente y que el transcript anterior sigue visible.
4. Ejecutar `/compact` en reposo y luego `/compact prioriza decisiones y errores pendientes`.
5. Cerrar y reabrir la sesión; confirmar que el resumen activo, transcript y archivo de compactación persisten.
6. Probar Esc durante una compactación manual.
