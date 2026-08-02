# 093 — VPS, red resiliente y releases manuales

Fecha: 2026-08-02

## Objetivo

Corregir envíos que parecían ignorar Enter en terminales VPS/SSH, evitar que una
conexión de proveedor caída deje mensajes escritos atrapados y crear un flujo
manual de GitHub Actions que compile y publique releases desde una única versión
definida en código.

## Enter confiable en PTY/SSH

Algunos clientes SSH y combinaciones PTY/terminfo entregan la tecla Return como
`Ctrl+M` (`CR`) en lugar de `KeyEnter`. Tcell preservaba esa diferencia y Lilith
no tenía un atajo para `Ctrl+M`, por lo que la pulsación llegaba al runtime pero
no ejecutaba el envío.

`tcellKeyMsg` ahora normaliza tanto `KeyEnter` como `KeyCtrlM` a `uikit.KeyEnter`.
Se conserva `Ctrl+J` para la navegación histórica de paneles y para el fallback
de pegado CRLF; no se mezclaron esos comportamientos.

## Cola durante errores de red

Mientras existe un turno activo, Enter crea steering para la siguiente frontera
segura. Antes, si el proveedor terminaba con error, el turno se cerraba pero la
cola no se drenaba. El texto seguía guardado internamente y desde la interfaz
parecía que Enter no había hecho nada.

Los errores de proveedor y de ejecución de herramientas ahora son fronteras
válidas: después de persistir el error, `drainFollowUp` consume el siguiente
steering/follow-up y lo inicia como turno nuevo. Los mensajes restantes conservan
su orden y se entregan en las fronteras posteriores.

## Transporte resiliente para VPS

`openai.NewClient` deja de usar `http.Client.Timeout = 5m`, porque ese valor era
un límite total que también cancelaba streams largos aunque siguieran recibiendo
contenido. El transporte nuevo usa:

- dial máximo de 15 segundos;
- TCP keepalive con primer sondeo a 30 segundos, intervalo de 15 segundos y
  cuatro sondeos sin respuesta;
- handshake TLS máximo de 15 segundos;
- espera de headers máxima de dos minutos;
- conexiones idle reutilizables durante 90 segundos;
- ningún timeout total para una generación activa.

Tanto Chat Completions SSE como Codex Responses SSE pasan por el mismo lector con
watchdog. Cada línea recibida —incluidos comentarios keepalive— reinicia el reloj.
Si no llega ningún byte durante cuatro minutos, se cierra el body para desbloquear
la lectura y el fallo entra en la política existente de reintentos transitorios.
Sólo se reintenta automáticamente cuando todavía no se emitió contenido, evitando
duplicar texto o tool calls a mitad de respuesta.

En Unix también se captura `SIGHUP` junto con SIGTERM/Interrupt para que una
terminal SSH cerrada detenga el runtime de forma controlada. La conversación ya
se persiste en vivo y puede retomarse con `li --continue`. Para conservar el mismo
proceso y volver a adjuntar exactamente la TUI tras perder la conexión SSH sigue
siendo necesario ejecutar Lilith dentro de `tmux`, `screen` o una sesión similar;
ninguna TUI ligada directamente a un PTY desaparecido puede recibir teclas.

## Versión única

La fuente de verdad queda en:

```text
internal/version/version.go
```

`version.Current` contiene SemVer sin prefijo `v`, inicialmente `0.1.0`. El CLI
usa ese valor como versión predeterminada y `cmd/build` lo valida y lo inyecta en
todos los binarios mediante ldflags. El nuevo comando:

```bash
go run ./cmd/build version
```

imprime únicamente la versión, por lo que scripts y GitHub Actions no necesitan
parsear código ni depender de tags previos.

## Workflow manual de release

`.github/workflows/release.yml` se ejecuta sólo mediante `workflow_dispatch` y:

1. configura la versión de Go declarada en `go.mod`;
2. lee `cmd/build version`;
3. rechaza publicar si el tag o release ya existe;
4. ejecuta `go test ./...`;
5. ejecuta `go run ./cmd/build build`;
6. genera `dist/SHA256SUMS.txt`;
7. crea el tag `v<versión>` y el GitHub Release con todos los binarios.

Para una versión nueva sólo se cambia `version.Current`, se hace commit/push y se
ejecuta manualmente **Publicar release** desde Actions.

## Pruebas añadidas

- `Ctrl+M` de Tcell se traduce a Enter.
- un error de proveedor consume el mensaje escrito durante el turno y arranca la
  siguiente petición, en vez de dejarlo oculto en la cola.
- el cliente de producción no tiene timeout HTTP total y sí límites de conexión.
- una conexión SSE silenciosa se desbloquea al vencer el watchdog.
- `cmd/build version` es una acción válida y la versión central cumple SemVer.

## Validación realizada

El entorno disponible tiene Go 1.23.2 y no posee acceso de red para descargar la
toolchain Go 1.24 ni dependencias Tview/Tcell. En una copia temporal con la
directiva `go` bajada sólo para la prueba se ejecutó sin red:

```text
GOTOOLCHAIN=local GOPROXY=off go test ./internal/providers/openai ./cmd/build
```

Ambos paquetes pasan. Los archivos TUI fueron formateados y sus pruebas de
regresión quedan incluidas, pero la suite completa debe ejecutarse en GitHub
Actions o en un equipo con Go 1.24 y módulos disponibles.

## Pruebas manuales recomendadas

1. Entrar a un VPS por SSH con distintos clientes y enviar usando Return varias
   veces seguidas; cada pulsación debe iniciar o encolar el mensaje.
2. Durante una respuesta, cortar temporalmente la salida del VPS a internet,
   escribir otro mensaje y pulsar Enter; la TUI debe seguir aceptando edición y
   el mensaje debe arrancar al cerrarse/reintentarse la petición fallida.
3. Pulsar Esc durante una conexión caída y confirmar cancelación inmediata.
4. Ejecutar Lilith dentro de `tmux`, desconectar SSH, reconectar y volver a
   adjuntar la sesión.
5. Cambiar `version.Current`, ejecutar el workflow manual y verificar los cinco
   binarios, `SHA256SUMS.txt`, tag y release.
