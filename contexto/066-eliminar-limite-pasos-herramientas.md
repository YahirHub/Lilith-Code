# 066 - Eliminar límite rígido de pasos de herramientas

## Problema

El runtime del chat mantenía `toolSteps` y detenía cualquier turno que superara `maxToolSteps = 60`. Al alcanzar el techo se insertaban resultados sintéticos de error para las tool calls pendientes y el usuario debía pedir manualmente que el agente continuara.

Ese límite es inadecuado para tareas de agente largas: una implementación legítima puede requerir muchas iteraciones de lectura, búsqueda, edición, verificación y corrección, especialmente con herramientas lazy o flujos de Plan/Build.

## Cambio

Se eliminó por completo el contador `toolSteps`, la constante `maxToolSteps`, sus incrementos/resets y la rama que interrumpía el turno al superar 60 pasos.

El agente puede continuar encadenando herramientas mientras el modelo siga solicitándolas y el turno permanezca activo.

## Protecciones que se conservan

Eliminar el techo numérico no elimina los mecanismos reales de control:

- `Esc` cancela el contexto completo del turno.
- Las herramientas pueden conservar sus propios timeouts y validaciones.
- Plan Mode sigue aplicando su política de herramientas y shell de solo lectura.
- Los resultados tardíos siguen invalidados mediante `activeTurnID`/context cancellation.
- El transporte Codex continúa reparando pares `function_call` huérfanos cuando un turno se interrumpe por causas reales como cancelación o reemplazo de sesión.

## Compatibilidad

No cambia el protocolo de tools ni los schemas enviados a proveedores. Tampoco afecta prompt caching: se elimina únicamente una condición local del loop de ejecución.
