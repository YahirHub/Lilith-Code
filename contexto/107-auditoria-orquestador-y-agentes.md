# 107 · Auditoría integral del orquestador y agentes

# Fecha

2026-08-03

# Objetivo

Revisar de punta a punta la ejecución de agentes de Lilith y endurecer los
invariantes que conectan descubrimiento, tool calls, foreground/background,
paralelismo, nesting, cancelación, reanudación, worktrees, persistencia, eventos
y restauración de sesiones.

# Decisiones tomadas

- Mantener un único runtime en `internal/subagents`; invocaciones directas, la
  herramienta `Agent` y agentes anidados pasan por `subagents.Dispatch`.
- Tratar la desactivación de background como una conversión real a foreground,
  no como un worker background esperado de forma síncrona.
- Considerar provider, modelo y worktree persistidos como identidad de una
  reanudación. La selección actual del padre no puede reescribir esa identidad.
- Fallar de forma cerrada cuando el worktree de una tarea ya no existe, evitando
  que el agente actúe accidentalmente sobre el checkout principal.
- Aislar cada conversación mediante una generación atómica de eventos y un
  contexto de cancelación de sesión.
- Persistir paneles background terminales y reconstruir la notificación pendiente
  al reabrir una sesión; identificar cada ejecución por `task_id + finished_at`.
- Serializar únicamente las fronteras mutables de hooks/checkpoints durante un
  lote paralelo, conservando el paralelismo de los agentes independientes.
- Añadir gates específicos de agentes/orquestador al workflow de release en
  Windows y ejecutar su runtime con detector de carreras en Linux.

# Arquitectura actual

```text
Agent / Task / @agente
        ↓
subagents.Dispatch
  ├─ foreground → Run(ctx) → resultado final
  └─ background → StartBackground(sessionCtx) → task_id inmediato
                                      ↓
                               eventos lifecycle
                                      ↓
                     canal TUI ligado a generación de sesión
                                      ↓
                  AgentPanel persistido + notificación única
```

Los lotes formados únicamente por llamadas `Agent` se ejecutan concurrentemente.
Cada resultado ocupa la posición de su tool call original antes de regresar al
proveedor. Los agentes anidados reciben `ParentTaskID`, profundidad y el mismo
árbol de cancelación.

Las sesiones hijas viven bajo el directorio de configuración por proyecto. Cada
`task_id` posee un lock en proceso mientras está activo y se guarda mediante un
temporal único con permisos `0600`, `Sync`, cierre y reemplazo.

# Librerías usadas

No se añadieron dependencias. Se reutilizan:

- `context`, `sync`, `sync/atomic`, `os` y `time` de la librería estándar;
- runtime existente de `internal/subagents`;
- persistencia de sesiones y componentes TUI existentes;
- GitHub Actions con la toolchain declarada en `go.mod`.

# Archivos importantes modificados

- `internal/subagents/runtime.go`
- `internal/subagents/runtime_test.go`
- `internal/subagents/store.go`
- `internal/tui/chat.go`
- `internal/tui/plan_mode.go`
- `internal/tui/agent_panel.go`
- `internal/tui/background_agent_recovery_test.go`
- `.github/workflows/release.yml`
- `README.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `tareas/completado-24-auditoria-orquestador-agentes.md`

# Problemas encontrados

- Un fallo background anterior a `EventStarted` podía dejar el panel visible en
  estado ejecutándose indefinidamente.
- Desactivar background quitaba el detach, pero conservaba políticas y eventos
  de background.
- Una reanudación resolvía primero el provider/modelo actual y podía ignorar los
  persistidos.
- Un worktree eliminado podía degradar la reanudación al checkout principal.
- Dos reanudaciones simultáneas podían escribir la misma sesión hija y compartir
  un nombre temporal; en background el intento perdedor incluso podía emitir un
  `failed` con el mismo `task_id` y ensuciar el panel de la ejecución válida.
- `/clear` o cargar otra sesión no aislaba eventos atrasados de workers anteriores,
  incluidos eventos que ya estaban encolados o agrupados por la TUI.
- Al descartar correctamente esos eventos antiguos, la sesión anterior podía
  conservar un panel `running` para siempre si se cerraba, cambiaba o salía de
  Lilith con un agente activo.
- Una finalización background podía perderse al reiniciar o añadirse después del
  prompt actual, convirtiéndose en la última instrucción vista por el modelo.
- Replays de eventos terminales duplicaban el texto de error en el panel.
- Un error de infraestructura posterior a `EventStarted`, por ejemplo al guardar
  el transcript después de una ronda de herramientas, podía terminar `Run` sin
  publicar un evento terminal.
- Las notificaciones creadas antes de `finished_at` evitaban un duplicado al
  actualizar, pero también podían silenciar reanudaciones futuras del mismo
  `task_id`.
- El prompt de subagentes abría dos veces `<code_intelligence>` y sólo lo cerraba
  una vez.
- El workflow no tenía un gate Windows dedicado a agentes/TUI ni una prueba race
  específica del orquestador.

# Soluciones implementadas

- `StartBackground` garantiza un evento terminal sintético para errores tempranos
  si `Run` aún no emitió uno.
- `Dispatch` aplica de manera central la política foreground/background.
- La reanudación toma provider/modelo del registro hijo y valida el worktree antes
  de iniciar el modelo.
- Un registro de tareas activas rechaza ejecución concurrente del mismo `task_id`;
  `StartBackground` reserva el identificador antes de devolver el control y los
  guardados usan temporales únicos, permisos restrictivos y validación de ruta.
- `/clear` cancela el contexto background anterior e incrementa una generación;
  cada evento viaja en un sobre con esa generación, por lo que también se
  descartan correctamente eventos ya encolados o agrupados.
- Antes de cambiar de sesión o salir, los paneles todavía activos se cierran como
  cancelados y se persisten en la sesión anterior.
- Las finalizaciones background se deduplican, persisten y se recuperan desde los
  paneles. En una nueva solicitud se insertan antes del prompt actual. Los
  marcadores antiguos se migran a `finished_at` para no silenciar una reanudación
  posterior del mismo identificador.
- Desde `EventStarted`, un guard diferido garantiza un único evento terminal aun
  cuando falle persistencia u otra infraestructura después de iniciar el panel.
- Los callbacks de hooks/checkpoints se serializan en lotes paralelos.
- `AgentPanel` vuelve idempotentes los eventos terminales y el prompt de código
  contiene un único bloque `<code_intelligence>`.
- Se añadieron pruebas para fallos tempranos, foreground forzado, provider/modelo
  persistidos, worktree ausente, cancelación anidada, paralelismo y orden, resume
  exclusivo antes del detach, fallos de persistencia posteriores al inicio,
  recuperación y migración de notificaciones, cierre de paneles al cambiar de
  sesión, separación generacional e idempotencia.

# Pendientes

- Ejecutar la suite completa con Go 1.24 y dependencias oficiales en los runners
  Windows/Linux; el entorno de entrega no dispone de red ni módulos en caché.
- Realizar una prueba manual en Windows Terminal y una sesión SSH: dos agentes
  paralelos, uno background, `/clear`, reanudación y cancelación de un árbol
  anidado.

# Próximos pasos

1. Ejecutar manualmente el workflow **Publicar release** sin crear release si se
   desea validar primero en una rama de prueba.
2. Confirmar que los nuevos jobs Windows y `-race` pasan sin flakes.
3. Probar `@agente`, tool call `Agent`, background y `task_id` con un proveedor
   real antes de cambiar la versión pública.
