# Tarea 24 · Auditoría integral del orquestador y subagentes

## Estado

Completada y documentada.

## Objetivo

Auditar y endurecer el runtime de orquestación de Lilith: descubrimiento de agentes, ejecución foreground/background, paralelismo, nesting, resume, cancelación, persistencia, eventos, políticas y notificaciones al agente padre.

## Criterios de aceptación

- Los fallos tempranos de una tarea background siempre producen un evento terminal visible.
- Desactivar background fuerza semántica foreground completa, sin resultados duplicados ni estado `running` falso.
- Reanudar una tarea usa primero su provider/modelo persistidos y falla de forma segura si su worktree ya no existe.
- Las finalizaciones background sobreviven a un reinicio hasta ser entregadas una sola vez al modelo padre.
- Hay pruebas de regresión para los escenarios anteriores, además de paralelismo, cancelación y políticas existentes.
- `contexto/` y documentación reflejan el comportamiento vigente.
- El prompt de código y los eventos terminales son estructuralmente idempotentes.
- CI prueba el orquestador/agentes en Windows y con `-race` en Linux.

## Resultado

Se corrigieron los fallos de lifecycle background y posteriores a `EventStarted`, foreground forzado, reanudación, aislamiento de worktree, reserva exclusiva antes del detach, persistencia atómica, contaminación por eventos encolados entre sesiones, cierre durable de paneles al cambiar/salir, orden y migración de notificaciones, idempotencia visual y bloque de inteligencia de código. Se añadieron pruebas y gates de CI específicos.
