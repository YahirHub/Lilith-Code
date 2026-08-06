# 123. Corregir activación del dominio Debugger de Chromedp

## Problema

La primera entrega experimental del navegador no compilaba con la versión fijada
de `github.com/chromedp/cdproto`.

`debugger.Enable()` no implementa directamente `chromedp.Action`: su método
`Do(context.Context)` devuelve un `cdp.UniqueDebuggerID` además del error. Por
ello no puede pasarse como argumento directo de `chromedp.Run`, cuyo contrato
requiere acciones cuyo `Do` devuelva únicamente `error`.

El error afectaba la pestaña inicial y cada pestaña creada posteriormente, lo que
impedía compilar `internal/browser` y, por dependencia, `cmd/li`, `internal/tools`,
`internal/subagents` e `internal/tui`.

## Corrección

- Se añadió `enableDebugger`, un adaptador `chromedp.ActionFunc`.
- El adaptador ejecuta `debugger.Enable().Do(ctx)`, descarta de forma explícita el
  identificador del depurador y propaga el error.
- La pestaña inicial y las pestañas nuevas utilizan el mismo adaptador.
- En pestañas nuevas, el listener de eventos se registra antes de habilitar los
  dominios CDP para no perder los eventos `Debugger.scriptParsed` iniciales.
- Se añadió una prueba de compilación que exige que el adaptador implemente
  `chromedp.Action`.

## Archivos

- `internal/browser/manager.go`
- `internal/browser/manager_test.go`

## Validación

Realizada en este entorno:

- `gofmt` sobre los archivos modificados.
- `git diff --check`.
- Revisión de que no quede ningún `debugger.Enable()` pasado directamente a
  `chromedp.Run`.

La suite Go integral debe ejecutarse en un equipo con Go 1.25.12 y las
dependencias descargadas:

```bash
go test -mod=readonly -tags=grammar_set_core -count=1 -timeout=15m ./...
go vet -mod=readonly -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
```
