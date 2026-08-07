# 134. Corregir fixture de recuperación `reasoning_content`

## Problema

El runner de Windows y GitHub Actions ejecutan correctamente la suite completa de
Lilith v0.3.2, pero dos pruebas de `internal/tui` fallaban:

- `TestReasoningCarryForwardErrorContinuesSilentlyAfterToolBoundary`
- `TestReasoningCarryForwardRecoveryDoesNotLoopForever`

Ambas esperan que, después del HTTP 400 específico de `reasoning_content`, la
recuperación automática vuelva a entrar en `runTurn()` y cree una petición nueva.

## Causa

La lógica de producción inicializa cada turno mediante `beginTurnMode()`, que
congela el proveedor y modelo activos en:

- `m.turnProvider`
- `m.turnModel`

El helper de tests `primeTestRequest`, en cambio, sólo construía el contexto,
`activeTurnID`, `activeRequestID` y `streaming`. Dejaba proveedor y modelo vacíos.

Cuando la prueba alcanzaba `recoverReasoningCarryForward()`, ésta llamaba
correctamente a `runTurn()`, pero `runTurn()` ejecutaba
`FindProvider(m.turnProvider)` con un ID vacío. Como debe hacer ante un turno sin
proveedor, cerraba ese turno y devolvía `nil`. El test, por tanto, estaba
simulando un estado imposible en producción.

## Corrección

`primeTestRequest` ahora replica también el snapshot de proveedor/modelo de
`beginTurnMode()` cuando el `AppContext` del test tiene una selección válida.
Sólo rellena los campos que todavía están vacíos, por lo que no pisa snapshots
que una prueba haya configurado explícitamente.

Los tests con un `AppContext` deliberadamente mínimo/sin proveedores conservan
el comportamiento previo: `Providers.Active()` devuelve un snapshot vacío y el
helper no inventa ningún proveedor.

Se añade además una regresión específica que exige que `newInputTestChat` +
`primeTestRequest` produzcan `turnProvider="test"` y `turnModel="modelo"`.

## Alcance

No cambia la lógica de recuperación de producción, el cliente del proveedor, la
TUI ni el protocolo. Es una corrección del fixture para que la suite represente
el mismo estado de turno que la aplicación real.

## Validación

- `gofmt` aplicado.
- `git diff --check` sin errores.
- No cambia `go.mod` ni `go.sum`.
- El entorno de entrega sólo dispone de Go 1.23.2 y no puede descargar el
  toolchain 1.25.12; por eso la ejecución de `go test` debe confirmarse en el
  runner/Windows que ya dispone del toolchain correcto.

## Archivos

- `internal/tui/chat_test_helpers_test.go`
- `contexto/134-corregir-fixture-recovery-reasoning.md`
