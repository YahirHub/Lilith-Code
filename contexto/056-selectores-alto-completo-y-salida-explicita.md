# 056 · Selectores a alto completo y salida explícita

Fecha: 2026-07-28

## Objetivo

Aprovechar todo el alto disponible de la terminal en `/models` y `/history`, y eliminar los atajos de teclado que podían cerrar o suspender Lilith accidentalmente.

## `/models` y `/history`

`selection_surface.go` ya no reserva una cantidad fija aproximada de filas. Calcula la altura renderizada real de cabecera, búsqueda, footer y error, y entrega **todo el resto de la terminal** al viewport de resultados.

- La búsqueda y las tarjetas siguen ocupando el mismo ancho útil completo.
- La selección sigue usando la altura renderizada real de cada tarjeta.
- El área de resultados se rellena hasta el footer, que queda al fondo de la terminal.
- Si existen más resultados, se muestran tantos como físicamente caben antes de comenzar el scroll.
- Si hay pocos resultados, el espacio restante pertenece al viewport en vez de dejar el layout terminado a media pantalla.

## Salida de Lilith

La salida del proceso queda deliberadamente explícita:

- `/exit`: única orden que cierra Lilith.
- `/exit` se ejecuta inmediatamente incluso durante streaming; no entra a steering/follow-up. Antes de salir cancela el turno activo para invalidar requests y herramientas en curso.
- `Ctrl+C`: sin acción dentro de la TUI. No sale, no cancela y no borra el editor.
- `Ctrl+Z`: sin acción dentro de la TUI. No suspende el proceso en Unix.
- `Ctrl+D`: ya no es una salida del chat; conserva usos propios de pantallas específicas, por ejemplo borrar una sesión en `/history`.
- `Esc`: continúa siendo la interrupción segura del turno activo y el regreso desde pantallas secundarias.

También se retiraron las salidas por `Ctrl+C` de `/config`, `/models`, `/history`, onboarding y pantallas de proveedores/login. El status bar y `/help` muestran ahora `/exit salir`.

## Archivos principales

- `internal/tui/selection_surface.go`
- `internal/tui/chat.go`
- `internal/tui/commands.go`
- `internal/tui/status_bar.go`
- `internal/tui/model_selector.go`
- `internal/tui/history_screen.go`
- pantallas de configuración/login/onboarding/proveedores
- pruebas de selectores y atajos

## Validación

Con Go 1.24+:

```bash
go test ./internal/tui
go test ./...
go vet ./...
go build ./cmd/li
```

Prueba manual recomendada: redimensionar Windows Terminal a varias alturas, abrir `/models` y `/history`, verificar que el footer quede en la zona inferior y que al aumentar la altura aparezcan más resultados sin perder el foco. Durante una tarea, comprobar que `Ctrl+C`/`Ctrl+Z` no cambian el estado, `Esc` cancela y `/exit` termina el CLI inmediatamente.


## Nota de evolución

La política de salida explícita (`/exit`) de este cambio continúa vigente. La
implementación de altura de `selection_surface.go` ya no es la utilizada por
`/models` ni `/history`; fue reemplazada por un selector compacto con
`viewport` real, descrito en `057-selector-viewport-modelos-history.md`.
