# 125. Corregir el ciclo de vida de los contextos Chromedp

## Problema reproducido

En Windows, `browser start` abría correctamente Chrome, pero la sesión quedaba
inutilizable inmediatamente después:

```text
browser navigate   -> error: context canceled
browser snapshot   -> error: context canceled
browser screenshot -> error: context canceled
```

`browser status` mostraba `attached: false` y `tabs: null`, aunque `start` había
devuelto una pestaña inicial.

## Causa

El primer `chromedp.Run` de la sesión se ejecutaba sobre un hijo temporal creado
con `context.WithTimeout(browserCtx, ...)`. Al terminar `Start`, el `defer` de ese
contexto temporal lo cancelaba.

El primer `Run` es especial: Chromedp asigna allí el navegador y el executor del
target. Cancelar el contexto usado para esa primera ejecución destruye ese target
y provoca que cualquier operación posterior reciba `context canceled`.

La misma construcción se repetía al crear una pestaña nueva, por lo que una
pestaña podía abrirse y quedar cancelada antes de su siguiente acción.

## Corrección

- El primer `Run` del navegador se ejecuta directamente sobre `browserCtx`, que
  permanece vivo durante toda la sesión lógica.
- El límite de arranque se conserva mediante un temporizador que sólo cancela el
  contexto persistente cuando el arranque supera realmente el tiempo máximo.
- El primer `Run` de cada pestaña nueva o adoptada también usa su contexto
  persistente directamente.
- Los contextos con timeout quedan reservados para acciones posteriores, cuando
  el navegador y el target ya tienen su executor inicializado.
- La pestaña inicial ya no conserva como cancelador individual la función que
  cierra toda la sesión.
- `status` ya no oculta errores CDP ni devuelve `tabs: null`: devuelve una lista
  vacía y un campo `cdp_error` cuando el navegador dejó de responder.
- `attached` representa ahora si el contexto CDP sigue activo. El nuevo campo
  `remote_attached` distingue una sesión adoptada mediante endpoint remoto de un
  navegador lanzado por Lilith.
- Al adoptar una pestaña existente se habilitan Network, Runtime y Debugger antes
  de registrarla como pestaña activa.
- Las operaciones del dominio `Target` (`getTargets`, `createTarget` y
  `closeTarget`) usan el executor del navegador; las operaciones Network y
  Debugger que devuelven varios valores usan explícitamente el executor del
  target. Así no dependen de ejecutar comandos CDP directos sobre un contexto
  sin executor.
- La cancelación de una llamada de herramienta sólo cancela su operación hija;
  nunca cierra el contexto persistente de la sesión.

## Pruebas añadidas

- El helper de arranque conserva exactamente el mismo contexto tras una ejecución
  exitosa y no lo cancela al regresar.
- Cancelar el contexto de una solicitud cancela la operación actual sin cerrar el
  contexto CDP persistente.
- Un timeout real cancela el contexto persistente y se informa como
  `context deadline exceeded`.
- La prueba con navegador real ejecuta operaciones separadas y consecutivas:
  `start`, `navigate`, `snapshot`, `status/tabs` y `screenshot`.
- La prueba verifica además que `attached` permanezca en `true` y que la captura
  generada no esté vacía.

Prueba manual recomendada en Windows:

```powershell
$env:LILITH_BROWSER_INTEGRATION = "1"
$env:LILITH_BROWSER_EXECUTABLE = "C:\Program Files\Google\Chrome\Application\chrome.exe"
go test -tags=grammar_set_core ./internal/browser -run TestBrowserIntegrationStartAndSnapshot -v
```

Después, dentro de Lilith, probar llamadas separadas con el mismo `session_id`:

```text
browser start      session_id=jsecure headless=false profile_mode=temporary
browser status     session_id=jsecure
browser navigate   session_id=jsecure url=https://jsecure.juarez.admvo.org/
browser snapshot   session_id=jsecure
browser screenshot session_id=jsecure path=dashboard.png
```

## Archivos

- `internal/browser/manager.go`
- `internal/browser/manager_test.go`
- `internal/browser/types.go`
- `internal/tools/browser.go`
