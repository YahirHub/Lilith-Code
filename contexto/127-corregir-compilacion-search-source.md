# 127. Corregir compilación de `search_source`

## Error detectado en Windows

Después del commit que corrigió los IDs caducados de scripts, `go run` y la
suite completa no compilaban:

```text
internal/browser/actions.go:17:2: "github.com/chromedp/cdproto/runtime" imported as cdpruntime and not used
internal/browser/actions.go:452:56: undefined: cdp.ScriptID
internal/browser/actions.go:465:9: not enough return values
have ([]map[string]any, bool)
want ([]map[string]any, bool, error)
```

## Causa

El proyecto fija `github.com/chromedp/cdproto` en el commit
`65de8f5d025b`. En esa versión, `Debugger.searchInContent` recibe
`runtime.ScriptID`; no existe `cdp.ScriptID` para esa operación.

Además, `formatSearchMatches` devuelve sólo los resultados y el indicador de
truncamiento, mientras que `Session.SearchSource` también debe devolver un
`error`.

La prueba de limpieza de contextos repetía el mismo uso incompatible de
`cdp.ScriptID`, por lo que habría fallado al compilar una vez corregido el
archivo de producción.

## Corrección

- Se usa `cdpruntime.ScriptID(scriptID)` al ejecutar
  `Debugger.searchInContent`.
- Se elimina el import de `cdp` que quedó sin uso en `actions.go`.
- `SearchSource` convierte los dos valores del formateador en su contrato de
  tres retornos y devuelve `nil` como error en la ruta exitosa.
- La prueba de reconstrucción del inventario usa también
  `cdpruntime.ScriptID` y elimina su import de `cdp`.

## Archivos

- `internal/browser/actions.go`
- `internal/browser/manager_test.go`

## Pruebas recomendadas en Windows

```powershell
go mod tidy
go test -mod=readonly -tags=grammar_set_core -count=1 -timeout=15m ./...
go run .\cmd\li\main.go
```

Después del arranque, repetir la validación funcional:

```text
browser scripts       session_id=jsecure
browser search_source session_id=jsecure script_id=<ID_ACTUAL> query=geocode
```
