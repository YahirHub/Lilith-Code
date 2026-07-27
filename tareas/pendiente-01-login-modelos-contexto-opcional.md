# Tarea 01 — /login con modelos y contexto opcionales

## Estado

pendiente

## Objetivo

Ajustar el alta de proveedores compatibles desde `/login` para que la lista de
modelos y los límites de contexto sean opcionales. Si el campo se deja vacío y
se pulsa Enter, Lilith debe consultar `GET {baseURL}/models`, guardar los
modelos devueltos y finalizar el alta sin exigir una segunda confirmación.

## Implementado

- [x] Entrada manual `modelo` o `modelo=contexto`.
- [x] Enter vacío inicia `GET {baseURL}/models`.
- [x] Los modelos descubiertos se guardan automáticamente sin segundo Enter.
- [x] La TUI explica que modelos y contexto son opcionales.
- [x] Prueba aislada del cliente `/models` con servidor HTTP local.
- [x] Pruebas del flujo TUI añadidas al repositorio.
- [ ] Ejecutar `go test ./...` en un entorno con Go 1.24+ y dependencias
      disponibles.

## Validación realizada

- `gofmt`: correcto.
- `git diff --check`: correcto.
- `internal/providers`: pruebas pasan usando una copia temporal con `go 1.23`
  y `GOPROXY=off`.
- `internal/tui`: no ejecutable en este sandbox porque las dependencias Charm no
  están cacheadas y el acceso a `proxy.golang.org` está bloqueado.

## Criterio para completar

Renombrar a `completado-01-login-modelos-contexto-opcional.md` después de que la
suite completa pase localmente y se confirme el flujo manual en la TUI.
