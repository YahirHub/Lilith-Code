# 109 · Estabilizar prueba de cancelación anidada en Windows

# Fecha

2026-08-04

# Objetivo

Corregir el fallo intermitente de `TestCancelParentTearsDownNestedAgentTree` observado al ejecutar `test.cmd` en Windows, sin ocultar una posible regresión real del orquestador.

# Decisiones tomadas

- Mantener la prueba de cancelación completa del árbol padre → hijo.
- Considerar que el hijo ha arrancado exactamente cuando su `Streamer.Stream` es invocado.
- Emitir esa señal de forma síncrona, antes de crear la goroutine que espera la cancelación.
- Esperar también el resultado del padre durante la fase de arranque para fallar inmediatamente si termina antes de crear al hijo.
- Usar temporizadores explícitos: 10 segundos para inicialización en equipos Windows lentos y 5 segundos para detener el árbol después de cancelar.
- Repetir la prueba cinco veces en el runner Windows del workflow de release.

# Arquitectura actual

La implementación productiva del orquestador no cambió. La prueba utiliza un `nestedCancelStreamer` controlado:

1. La primera petición devuelve una llamada a la herramienta `Agent`.
2. La segunda entrada a `Stream` representa el arranque real del agente hijo.
3. La señal de arranque se cierra síncronamente en esa frontera.
4. El stream hijo espera `ctx.Done()` y devuelve `context.Canceled`.
5. La prueba comprueba eventos terminales tanto del padre como del hijo.

# Librerías usadas

Sólo librería estándar de Go: `context`, `sync`, `testing` y `time`.

# Archivos importantes modificados

- `internal/subagents/runtime_test.go`
- `AGENTS.md`
- `.github/workflows/release.yml`
- `contexto/000-contexto-maestro.md`
- `contexto/109-estabilizar-prueba-cancelacion-anidada-windows.md`
- `tareas/completado-26-estabilizar-test-cancelacion-anidada-windows.md`

# Problemas encontrados

La prueba cerraba `childStarted` dentro de una goroutine recién creada. Por tanto, el timeout de dos segundos medía dos cosas diferentes:

- el tiempo real necesario para que el orquestador construyera el agente anidado;
- el tiempo que Windows tardaba en programar la goroutine auxiliar del fake streamer.

En equipos con disco, antivirus o scheduler cargado, el segundo factor podía provocar `nested child did not start` aunque `Stream` ya hubiera sido invocado correctamente.

Además, si el padre terminaba por un error real antes de iniciar al hijo, la prueba esperaba todo el timeout y mostraba un mensaje poco útil.

# Soluciones implementadas

- `nestedCancelStreamer.Stream` cierra `childStarted` de forma síncrona al recibir la petición hija.
- Se añadió un contador sincronizado de requests para diagnóstico.
- La prueba ahora observa simultáneamente:
  - arranque del hijo;
  - terminación prematura del padre;
  - vencimiento de la fase de arranque.
- Después de cancelar, espera un máximo separado de cinco segundos para que termine el árbol.
- El workflow Windows repite la prueba cinco veces después de la suite del orquestador.

# Pendientes

- Ejecutar `test.cmd` en el equipo Windows donde se reprodujo el fallo.
- Confirmar el nuevo gate al ejecutar manualmente el workflow **Publicar release**.

# Próximos pasos

1. Reemplazar el proyecto por el ZIP actualizado.
2. Ejecutar `test.cmd` desde CMD o PowerShell.
3. Si aparece otro fallo, conservar la salida completa: los nuevos mensajes distinguen terminación prematura, falta de arranque y falta de cancelación.
