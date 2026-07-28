# Fecha
2026-07-27

# Objetivo
Hacer que Ctrl+C detenga de inmediato un turno completo, incluidas las herramientas y procesos que éste lanzó, impedir que resultados tardíos reactiven el agente y permitir cambiar el modelo desde `/models` para que la siguiente petición del usuario use la nueva selección sin reiniciar Lilith.

# Decisiones tomadas
- Un turno de usuario tiene ahora un `context.Context` raíz compartido por el streaming del proveedor y todas sus herramientas.
- Cada turno recibe un `activeTurnID` monotónico. Los mensajes SSE y resultados de tools llevan ese ID; si el turno ya fue cancelado, sus resultados tardíos se ignoran.
- Ctrl+C invalida primero el ID del turno y después deja que el cierre físico de procesos ocurra en el goroutine de la herramienta. El `Update` de Bubble Tea no espera a `taskkill`, `Wait` ni al proveedor.
- Las tool calls que ya estaban registradas en el historial reciben un resultado sintético de cancelación para mantener válido el contrato OpenAI-compatible del siguiente request.
- Los `CommandPanel` vivos pasan inmediatamente al estado `canceled`.
- `run_terminal_command` recibe el contexto real del turno en vez de `context.Background()`.
- `shell.Run` distingue `Canceled` de `TimedOut` y reduce `WaitDelay` de 2 s a 150 ms después de un hard kill del árbol de procesos.
- El proveedor y modelo se capturan al comenzar cada turno. Una selección posterior nunca cambia una continuación de tool calls a mitad del mismo turno; se aplica al siguiente turno del usuario.
- `/models` persiste la fila ya validada por el selector directamente con `providers.Save` y actualiza `AppContext.Providers` en memoria. Se elimina la recarga remota innecesaria al elegir un modelo.

# Arquitectura actual
```text
submit / invokeSkill
    -> beginTurn()
       - snapshot provider/model
       - turnID
       - context.WithCancel
    -> runTurn()
       -> Client.Stream(turnCtx)
       -> chatStreamMsg{turnID}
       -> runTools(...)
          -> tools.Execute(turnCtx, ...)
          -> shell.Run(turnCtx, ...)
          -> toolResultsMsg{turnID}

Ctrl+C
    -> cancelTurn()
       - cancel context
       - activeTurnID = 0
       - paneles -> canceled
       - UI deja Pensando/Trabajando inmediatamente
       - cualquier resultado tardío con el ID viejo se descarta

/models
    -> selecciona fila del catálogo cargado
    -> providers.Save(activeProviderID, activeModelID)
    -> actualiza AppContext en memoria
    -> siguiente beginTurn() captura la nueva selección
```

# Librerías usadas
No se agregaron dependencias. Se reutilizan `context`, `os/exec`, Bubble Tea y la infraestructura existente de providers/tools/shell.

# Archivos importantes modificados
- `internal/tui/chat.go`
- `internal/tui/cmdpanel.go`
- `internal/tui/model_selector.go`
- `internal/tui/chat_cancel_model_test.go`
- `internal/tui/model_selector_test.go`
- `internal/shell/shell.go`
- `internal/shell/shell_test.go`
- `internal/tools/exec.go`
- `tareas/completado-04-paneles-altura-adaptativa.md`
- `tareas/en-proceso-05-cancelacion-instantanea-y-cambio-modelo.md`

# Problemas encontrados
- `runTools` ejecutaba cada herramienta con `context.Background()`, por lo que Ctrl+C cancelaba el request al proveedor pero no la herramienta activa.
- Al terminar más tarde una herramienta cancelada lógicamente, `toolResultsMsg` podía volver a entrar al flujo y llamar otra vez a `runTurn()`.
- El handler de Ctrl+C hacía un segundo `Resize()` después de cancelar. En historiales largos eso podía forzar una reconstrucción completa del transcript en el mismo frame y hacer que la cancelación se sintiera lenta.
- `SetActive` + `ReloadProviders` desde `/models` podía reconstruir el catálogo bundled y consultar red aunque la fila seleccionada ya estaba validada y cargada en memoria.
- El modelo se consultaba nuevamente desde la selección global en cada continuación del agente; una futura selección concurrente podría cambiar de modelo a mitad de un mismo turno.

# Soluciones implementadas
- Contexto raíz por turno y propagación hasta herramientas/shell.
- IDs de turno en eventos asíncronos para descartar resultados tardíos.
- Cancelación visual inmediata sin `Resize()` completo.
- Estado explícito `Canceled` en shell y panel de comando.
- `WaitDelay` corto después del hard kill.
- Snapshot de proveedor/modelo por turno.
- Persistencia inmediata y local del modelo desde `/models` sin refetch remoto.
- Pruebas de regresión de cancelación, resultados tardíos, shell cancelado y modelo del siguiente turno.

# Pendientes
- Ejecutar la suite TUI completa en un entorno con Go 1.24 y dependencias Charm disponibles.
- Validar manualmente en Windows con una app Electron que Ctrl+C cambie la UI al instante, cierre el árbol y que cerrar cualquier proceso residual no reactive Lilith.
- Validar cambio `gpt-5.5 -> deepseek-v4-flash` desde `/models` y confirmar el modelo en la siguiente petición real al endpoint.

# Próximos pasos
1. Ejecutar `go test ./...` y `go vet ./...` en Windows.
2. Abrir Electron desde una tool call y cancelar con Ctrl+C mientras sigue abierto.
3. Cambiar modelo con `/models`, enviar un mensaje nuevo y verificar en logs del proveedor/modelo que la siguiente petición usa la selección nueva.
