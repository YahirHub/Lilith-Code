# 124. Corregir arranque del navegador con ExecAllocator

## Problema reproducido

En Windows, `browser start` fallaba antes de lanzar Chrome, Edge o Brave con:

```text
error: iniciar navegador CDP: invalid exec pool flag
```

El fallo era independiente del ejecutable elegido, del modo visible/oculto y del
perfil temporal o persistente.

## Causa

La configuración de `chromedp.NewExecAllocator` añadía:

```go
chromedp.Flag("remote-debugging-port", 0)
```

`chromedp.Flag` sólo acepta valores `string` o `bool`. El entero `0` llega al
mapa interno de flags y el allocator lo rechaza con `invalid exec pool flag`
antes de construir `exec.Cmd`.

Además, no es necesario configurar ese flag: Chromedp agrega automáticamente
`--remote-debugging-port=0` cuando `remote-debugging-port` no está presente.

## Corrección

- Se eliminó el flag entero `remote-debugging-port`.
- Se centralizaron las opciones de arranque en `execAllocatorOptions`.
- Se configuró `WSURLReadTimeout` con la API tipada de Chromedp.
- En modo visible se desactivan también `hide-scrollbars` y `mute-audio`, que
  estaban habilitados por `DefaultExecAllocatorOptions` junto con `headless`.
- Se conserva `disable-background-networking=false` para permitir flujos reales
  de login y depuración de aplicaciones.
- `browser start` ahora respeta el `session_id` solicitado, por ejemplo
  `jsecure`, en vez de reemplazarlo siempre por un ID aleatorio.
- Los IDs de sesión se validan y se rechazan duplicados.

## Pruebas añadidas

- Regresión que inicia el allocator con un ejecutable falso y confirma que el
  error nunca vuelve a ser `invalid exec pool flag`.
- Validación de IDs de sesión.
- Prueba de integración opcional que abre un navegador real, navega a una página
  `data:` y obtiene un snapshot.

La prueba real se habilita con:

```powershell
$env:LILITH_BROWSER_INTEGRATION = "1"
$env:LILITH_BROWSER_EXECUTABLE = "C:\Program Files\Google\Chrome\Application\chrome.exe"
go test -tags=grammar_set_core ./internal/browser -run TestBrowserIntegrationStartAndSnapshot -v
```

## Archivos

- `internal/browser/manager.go`
- `internal/browser/manager_test.go`
- `internal/browser/types.go`
- `internal/tools/browser.go`
