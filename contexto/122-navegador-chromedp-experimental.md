# 122. Navegador persistente con Chromedp (experimental)

## Objetivo

Incorporar una herramienta de navegador comparable al flujo de navegación de un
agente de programación moderno: sesiones persistentes, perfiles aislados,
interacción con páginas, depuración DevTools y respuestas compactas para reducir
el consumo de contexto.

## Implementación

- Se creó `internal/browser` como runtime CDP compartido por proceso.
- Se registró la herramienta perezosa `browser` en `internal/tools`.
- Se añadieron detección de navegadores instalados y procesos CDP activos.
- Se admiten Chrome, Chromium, Edge, Brave, Vivaldi, Opera, Chrome for Testing y
  Chrome Headless Shell.
- Se añadieron perfiles `temporary`, `persistent` y `custom`.
- Los perfiles temporales se eliminan al cerrar la sesión; los persistentes viven
  dentro del directorio de configuración de Lilith.
- Se rechazan directorios que parezcan corresponder al perfil personal
  predeterminado.
- Las sesiones mantienen `session_id`, pestañas, referencias DOM y eventos de
  consola/red mientras Lilith permanezca abierto.
- `ShutdownAll` cierra navegadores lanzados y limpia perfiles temporales.

## Acciones de la herramienta

La herramienta permite descubrir/seleccionar navegador, iniciar/cerrar sesiones,
navegar, manejar pestañas, producir snapshots, leer texto/HTML, hacer clic,
escribir, seleccionar, enviar teclas, esperar, evaluar JavaScript, capturar
pantalla, consultar consola/red, recuperar cuerpos de respuesta, listar scripts,
buscar dentro de fuentes y consultar rendimiento.

## Ahorro de tokens

- Snapshot inicial acotado por texto y cantidad de elementos.
- Referencias estables `eN` para interactuar sin reenviar selectores largos.
- Snapshots delta con elementos añadidos, modificados y eliminados.
- Límites explícitos para consola, red, HTML, respuestas y fuentes.
- Los campos de contraseña y nombres sensibles no exponen su valor.

## Secretos

`fill_secret` abre un widget local enmascarado. El valor se aplica directamente al
selector dentro del proceso y no forma parte de los argumentos visibles para el
modelo, el historial ni la respuesta de la herramienta.

## Binario estático

Chromedp y el cliente CDP son Go puro. Lilith no incrusta un navegador: controla
un ejecutable externo. Por ello el CLI puede seguir compilándose con
`CGO_ENABLED=0`; Chrome/Chromium/Edge/Brave debe existir en el equipo o estar
disponible mediante `remote_url`.

## Dependencias

- `github.com/chromedp/chromedp v0.13.7`
- `github.com/chromedp/cdproto` y dependencias transitivas correspondientes.

## Validación realizada en este entorno

- `gofmt` aplicado.
- `git diff --check` sin errores.
- Revisión estática de schemas, ciclo de vida, snapshots y detección.
- Se corrigió el tratamiento de `network.GetResponseBody`, que devuelve bytes.

No fue posible ejecutar la compilación integral porque el entorno tiene Go 1.23.2,
el proyecto exige Go 1.25.12 y no hay acceso DNS para descargar el toolchain ni
los módulos nuevos. La entrega se considera una versión de prueba.

## Pruebas pendientes en un equipo con internet

```bash
go mod tidy
go test -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
```

Prueba funcional recomendada:

1. Ejecutar `discover`.
2. Iniciar Chrome/Edge visible con perfil temporal.
3. Navegar a una página local.
4. Solicitar snapshot, consola y red.
5. Interactuar mediante referencias `eN`.
6. Probar `fill_secret` sin incluir la contraseña en el prompt.
7. Cerrar la sesión y confirmar la eliminación del perfil temporal.
8. Repetir con perfil persistente y confirmar conservación de cookies.
