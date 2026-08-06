# 126. Corregir `search_source` e IDs de script caducados

## Problema reproducido

La batería de pruebas del navegador confirmó 26 de 27 acciones operativas. La
única acción bloqueada era `search_source`:

```text
scripts       -> script_id: "90"
search_source -> error: No script for id: 90 (-32000)
```

El fallo reaparecía incluso después de volver a ejecutar `scripts` porque el
inventario conservaba scripts de documentos anteriores en la misma pestaña.

## Causa

Los identificadores emitidos por `Debugger.scriptParsed` pertenecen al target y
al documento que los originó. Al navegar, recargar o reemplazar el documento,
CDP limpia los contextos de ejecución e invalida esos IDs.

Lilith mantenía `Tab.scripts` durante toda la vida de la pestaña. Por eso
`scripts` mezclaba archivos del documento actual con IDs ya inválidos y
`search_source` intentaba consultar uno que el depurador había descartado.

Además, la implementación descargaba el archivo completo con
`Debugger.getScriptSource` y después buscaba línea por línea, aunque CDP ya
ofrece `Debugger.searchInContent`.

## Corrección

- `Runtime.executionContextsCleared` invalida ahora todo el estado ligado al
  documento: scripts, referencias DOM, selectores, snapshot y metadatos
  derivados.
- Los nuevos eventos `Debugger.scriptParsed` reconstruyen el inventario sólo con
  scripts del documento vigente.
- `search_source` verifica que el `script_id` siga perteneciendo al inventario de
  la pestaña activa antes de consultar CDP.
- La búsqueda usa directamente `Debugger.searchInContent`, conserva búsqueda
  sensible o insensible a mayúsculas, límite de resultados y líneas visibles
  numeradas desde 1.
- Si ocurre una navegación entre `scripts` y `search_source`, el ID se elimina de
  la caché y se devuelve una instrucción clara para volver a listar scripts, en
  lugar de exponer `No script for id` como error crudo.
- El schema y las pautas de la herramienta aclaran que los IDs son efímeros y se
  deben renovar después de navegar o recargar.

## Pruebas añadidas

- La limpieza de contextos elimina scripts y referencias del documento anterior.
- El inventario se reconstruye con los nuevos eventos `scriptParsed`.
- `search_source` rechaza IDs que no pertenecen al documento actual sin ejecutar
  un comando CDP inválido.
- La conversión de resultados respeta líneas basadas en 1 y truncamiento.
- Se reconocen los errores habituales de ID de script inválido sin confundirlos
  con errores de contexto o transporte.
- La prueba opcional con navegador real busca un marcador JavaScript del
  documento actual mediante `search_source`.

## Archivos

- `internal/browser/manager.go`
- `internal/browser/actions.go`
- `internal/browser/manager_test.go`
- `internal/tools/browser.go`

## Validación manual recomendada en Windows

```text
browser start         session_id=jsecure headless=false profile_mode=temporary
browser navigate      session_id=jsecure url=https://jsecure.juarez.admvo.org/
browser scripts       session_id=jsecure
browser search_source session_id=jsecure script_id=<ID_ACTUAL> query=geocode
```

Después de cualquier `navigate`, `back`, `forward` o `reload`, ejecutar de nuevo
`scripts` antes de reutilizar `search_source`.

La prueba con navegador real continúa habilitándose con:

```powershell
$env:LILITH_BROWSER_INTEGRATION = "1"
$env:LILITH_BROWSER_EXECUTABLE = "C:\Program Files\Google\Chrome\Application\chrome.exe"
go test -tags=grammar_set_core ./internal/browser -run TestBrowserIntegrationStartAndSnapshot -v
```
