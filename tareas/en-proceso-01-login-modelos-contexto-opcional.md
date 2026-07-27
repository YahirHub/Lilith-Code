# Tarea 01 — /login con modelos y contexto opcionales

## Estado

en-proceso

## Objetivo

Ajustar el alta de proveedores OpenAI-compatible desde `/login` para que la
lista de modelos y los límites de contexto sean opcionales. Si el campo se deja
vacío y se pulsa Enter, Lilith debe consultar `GET {baseURL}/models`, guardar
los modelos devueltos y finalizar el alta sin exigir una segunda confirmación.

## Alcance

- Mantener el flujo OpenAI-compatible actual.
- Conservar la entrada manual `modelo` y `modelo=contexto`.
- Hacer explícito en la TUI que modelos/contexto son opcionales.
- Al dejar vacío, consultar `/models` y guardar el proveedor con lo descubierto.
- Añadir pruebas del descubrimiento y del flujo de login.
- Documentar la decisión en `contexto/`.

## Validación prevista

- `gofmt` sobre archivos Go modificados.
- `go test ./...` si el toolchain Go 1.24 está disponible.
- Prueba unitaria con endpoint HTTP local simulado.
