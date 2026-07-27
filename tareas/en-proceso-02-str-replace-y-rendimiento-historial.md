# Tarea 02 — Robustecer ediciones y optimizar historial TUI

## Estado

en-proceso

## Objetivo

Corregir el fallo de `str_replace` cuando un lote contiene una edición sin cambios
(`old == new`) y reducir el lag progresivo de la TUI conforme crece el historial,
sin recortar ni perder mensajes y manteniendo el scroll hacia conversaciones
anteriores.

## Implementado

- [x] `str_replace` ignora pares `old == new` y continúa con las ediciones reales.
- [x] Un lote formado sólo por no-ops termina correctamente sin reescribir el archivo.
- [x] `write_file` quedó limitado a crear archivos nuevos; no puede usarse como fallback para sobrescribir existentes.
- [x] `str_replace` y `apply_diff` exigen lectura previa cuando la TUI mantiene seguimiento de archivos vistos.
- [x] El prompt indica re-leer y reintentar una edición quirúrgica en vez de reescribir el archivo completo.
- [x] Los frames de `Pensando/Trabajando` ya no reconstruyen el transcript completo.
- [x] El streaming agrupa repintados del viewport a un máximo aproximado de 20 FPS; al leer historial con scroll manual baja a ~4 FPS para priorizar fluidez.
- [x] Se cachea el prefijo estable del historial durante el turno activo para no volver a renderizar Markdown antiguo en cada delta.
- [x] La caché del prefijo se libera al quedar ocioso para no duplicar permanentemente el historial en memoria.
- [x] El cálculo de uso de contexto queda cacheado y se invalida sólo al cambiar historial/proveedor/modelo.
- [x] El reloj visual de comandos pasa a 1 Hz porque sólo muestra segundos enteros.
- [x] El viewport conserva todo el transcript y respeta `userScrolled`; no se recortan mensajes antiguos.
- [x] Se añadieron pruebas de regresión para no-ops, overwrite, caché, throttling y scroll manual.

## Validación realizada

- `gofmt`: correcto.
- `git diff --check`: correcto.
- `go test ./internal/tools ./internal/providers`: correcto en copia temporal con `go 1.23` y `GOPROXY=off`.
- `go vet ./internal/tools ./internal/providers`: correcto bajo las mismas condiciones.
- La suite `internal/tui` no puede ejecutarse en este sandbox: `go.mod` exige Go 1.24 y las dependencias Charm no están disponibles localmente; el entorno tampoco puede resolverlas desde `proxy.golang.org`.

## Validación local pendiente

Ejecutar con Go 1.24+:

```powershell
go test ./...
go run .\cmd\li\
```

Después abrir/reanudar una conversación larga, iniciar una tarea con varias herramientas,
subir con PgUp/rueda mientras trabaja y confirmar que el desplazamiento permanece fluido.
