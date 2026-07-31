# 085 · Timeout de shell únicamente cuando se solicita

**Fecha**: 2026-07-31
**Estado**: aplicado

## Problema

`run_terminal_command` anunciaba y aplicaba un timeout predeterminado. La herramienta convertía la ausencia de `timeout_seconds` en 30 segundos y `internal/shell` volvía a sustituir un timeout de cero por el mismo valor. Esta doble política hacía imposible expresar de forma natural “sin límite” y empujaba a los modelos a inventar límites grandes, por ejemplo 720 segundos, que seguían cortando compilaciones que podían durar horas.

## Política vigente

- `timeout_seconds` es opcional.
- Si el argumento se omite, la herramienta envía `Timeout: 0`.
- `internal/shell` interpreta cero o valores negativos como ejecución sin fecha límite.
- Un número positivo continúa creando un `context.WithTimeout` y conserva `TimedOut=true` al expirar.
- La cancelación del turno mediante `Esc` o el contexto padre continúa terminando el grupo completo de procesos, incluidos hijos como compiladores, Docker, npm o scripts.
- El límite de salida permanece separado y sigue evitando consumo de memoria descontrolado; no es un timeout de ejecución.

## Cambios

- Eliminado `shell.DefaultTimeout`.
- Añadido `withOptionalTimeout`, que sólo crea deadline para duraciones positivas.
- El schema de `run_terminal_command` documenta `timeout_seconds` como límite duro opcional con mínimo 1.
- El prompt indica omitir el timeout en builds, instalaciones y suites largas salvo que se necesite deliberadamente un corte.
- Añadidas pruebas para ausencia de deadline y preservación de un timeout explícito.

## Verificación esperada

```bash
go test ./internal/shell ./internal/tools -count=1
go test ./... -count=1
go vet ./...
CGO_ENABLED=0 go build ./cmd/li
```

Prueba manual recomendada: ejecutar un comando largo sin `timeout_seconds`, confirmar que supera el límite anterior y cancelarlo con `Esc`; después ejecutar otro con un timeout positivo corto y comprobar que el panel termina en estado `timeout`.
