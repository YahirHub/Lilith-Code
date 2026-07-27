# 005 — Corrección: respuesta final tras ejecutar herramientas

## Síntoma

Al pedir `Crea un html de ejemplo`, Lilith ya ejecutaba correctamente
`write_file` y escribía `ejemplo.html`, pero después abría otro turno del
asistente y mostraba `El modelo no devolvió contenido`.

## Causa

El ciclo de herramientas estaba incompleto en la capa TUI:

1. El modelo emitía una tool call (`write_file`).
2. Lilith ejecutaba la herramienta y agregaba el mensaje `role: tool` al
   historial real.
3. Se lanzaba el siguiente turno para que el modelo resumiera el resultado.
4. Si el proveedor cerraba ese segundo turno sin texto, la UI trataba el caso
   como fallo genérico, aunque la acción ya había terminado correctamente.

En `original/packages/agent-runtime/src/tool-stream-parser.ts`, el flujo mantiene
el resultado visible de la herramienta y hace flush del texto acumulado. Lilith
necesitaba el equivalente mínimo: conservar un cierre determinista basado en el
resultado de la herramienta mutante.

## Solución

- Se agregó `toolFallback` al estado de chat.
- Tras recibir `toolResultsMsg`, Lilith prepara un resumen local compacto de
  resultados mutantes (`write_file`, `str_replace`) y errores de herramientas.
- Si el siguiente turno del modelo termina sin texto ni nuevas tool calls, la UI
  muestra ese resumen local en vez de `(el modelo no devolvió contenido)`.
- El fallback se limpia al iniciar una nueva solicitud o cuando el modelo sí
  devuelve texto real.

## Regla derivada

El mensaje `El modelo no devolvió contenido` sólo debe aparecer cuando de verdad
no hubo texto, tool calls ni resultado de herramienta utilizable. Una herramienta
mutante ejecutada correctamente ya es un cierre válido del turno.