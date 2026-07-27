# 036 — Indicador "Trabajando" diferenciado de "Pensando"

## Contexto
El shimmer "Pensando…" se pintaba tanto mientras el modelo respondía como
mientras se ejecutaban herramientas. No había señal visual de si el CLI
seguía activo o se había detenido tras un lote de tools.

## Decisión
- `RenderThinking` (púrpura) → esperando tokens del modelo.
- `RenderWorking` (verde/teal) → ejecutando herramientas.
- Nuevo campo `working bool` en `ChatModel`, seteado al despachar `runTools`
  y limpiado al recibir `toolResultsMsg`, en Ctrl+C y al iniciar turno.
- `thinkingTick` sigue corriendo mientras `thinking || working`.

Cuando ambos son false el shimmer desaparece, indicando parada real.

## Commit
feat(tui): shimmer "Trabajando" verde durante ejecución de herramientas
