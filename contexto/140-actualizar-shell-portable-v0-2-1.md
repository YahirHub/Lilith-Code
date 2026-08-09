# 140. Actualizar shell portable a v0.2.1

## Objetivo

Consumir la versión estable de `go-portable-shell` que amplía los controles de
embedding y corrige la expansión de `~/ruta` con separadores nativos en Windows.

## Integración

- `go.mod` fija `github.com/YahirHub/go-portable-shell v0.2.1` sin `replace`.
- La API usada por el toolbox de Lilith permanece compatible: `Config.Handler`,
  `Command`, `LookPath`, `ExitStatus` y `Status` no cambian.
- Lilith no activa `AllowHeredocs`; el alcance público continúa rechazándolos.
- `runPortable` reconoce `UnsupportedFeatureError` y lo traduce al exit code 2,
  igual que otras construcciones fuera del subconjunto portable.
- Una prueba de integración exige que `~/note.md` use el separador nativo del
  host, cubriendo la regresión detectada por la CI Windows de la librería.

## Motivo de la actualización

Aunque v0.2.1 es compatible con el uso actual, la actualización es material para
Lilith porque el fallback portable se ejecuta en Windows y puede recibir rutas
con tilde. Mantener v0.1.0 dejaría fuera esa corrección y todos los límites,
errores tipados y mejoras de proceso publicados por la librería.

## Validación ejecutada

- `go mod tidy -diff` sin diferencias y `go mod verify` sobre todos los módulos;
- suite completa y race detector con `-mod=readonly` y `grammar_set_core`;
- `go vet` y build estático Linux con `CGO_ENABLED=0`;
- build de Lilith y compilación de tests de `internal/shell` para Windows AMD64;
- compilación y carga cruzada de `cmd/li` para Android ARM64 con `-exec=true`;
- resolución pública de `v0.2.1` y checksum `h1` en `go.sum`, sin `replace` ni
  copia local del módulo.

La CI nativa Windows de `go-portable-shell v0.2.1` pasó con Go 1.24 y 1.25. En
este entorno Linux, el binario y los tests de Lilith para Windows se compilaron,
pero no se ejecutaron nativamente.
