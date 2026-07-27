# 039 — Fix definitivo del indicador "Trabajando" durante tool calls parciales

## Problema observado
Aunque el shimmer ya estaba fijado encima del input, seguía desapareciendo
mientras una herramienta mostraba su panel como `running`/`escribiendo`.

La causa no era el `View()` ni el viewport: era el estado del modelo.
Los transportes OpenAI/Codex emiten snapshots `Partial: true` mientras todavía
están llegando el nombre/argumentos de una tool call. En ese punto
`chatStreamMsg` creaba el panel y apagaba `thinking`, pero `working` no se
activaba hasta que terminaba por completo el stream de la tool call.

Durante ese intervalo quedaba:

```text
thinking = false
working  = false
```

El siguiente `thinkingTickMsg` veía ambos flags apagados, dejaba de programar
frames y el indicador desaparecía. En `write_file`, `apply_diff` o llamadas con
argumentos grandes ese hueco podía durar varios segundos.

## Corrección
En `internal/tui/chat.go`:

- cualquier `chatStreamMsg` que contenga `toolCalls`, incluso `Partial: true`,
  cambia inmediatamente a `working=true` y `thinking=false`;
- el ticker de shimmer continúa vivo durante todo el streaming de argumentos y
  durante la ejecución real de la herramienta;
- los deltas de reasoning mantienen `thinking=true` mientras no haya una tool
  activa;
- rutas de error/finalización limpian también `working` para evitar un estado
  visual pegado después de terminar.

El indicador sigue renderizándose mediante `pinnedActivityView`, por lo que
`Pensando` y `Trabajando` ocupan exactamente el mismo lugar encima del input.

## Pruebas de regresión
Se añadieron pruebas en `internal/tui/chat_tools_test.go` para comprobar que:

1. el primer snapshot parcial de `write_file` activa `Trabajando`;
2. el texto `Trabajando` está presente en el `View()`;
3. el `thinkingTickMsg` sigue programando frames con `working=true`;
4. el reasoning mantiene visible `Pensando`.

## Referencia TUI
Bubble Tea usa el patrón Model/Update/View: `Update` debe mantener el estado
real de la aplicación y `View` se vuelve a renderizar después de cada update.
Por eso el fix correcto es mantener un estado de actividad continuo durante la
fase parcial, no intentar forzar redraws manuales ni imprimir fuera del TUI.
