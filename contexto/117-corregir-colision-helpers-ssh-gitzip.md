# Fecha
2026-08-05

# Objetivo
Corregir el fallo de compilación introducido al integrar `ssh_remote` y `gitzip`, causado por helpers de argumentos declarados con nombres ya existentes dentro del paquete `internal/tools`.

# Decisiones tomadas
- No cambiar ni reutilizar las funciones heredadas `boolArg` e `intArg`, porque tienen semánticas distintas y ya son usadas por otras herramientas.
- Crear helpers compartidos con nombres inequívocos: `boolArgOr` e `intArgOr`.
- Conservar valores explícitos `false`, `0` y negativos; esto es necesario para opciones como `compression_level=0` y `timeout_seconds=-1`.
- Hacer que SSH y GitZip compartan esos helpers desde un archivo neutral, sin depender uno del archivo del otro.

# Arquitectura actual
- `internal/tools/args_optional.go` contiene extractores de argumentos con valor predeterminado.
- `internal/tools/ssh_remote.go` y `internal/tools/gitzip.go` consumen esos helpers.
- Los helpers históricos continúan en sus archivos originales sin cambios de comportamiento.

# Librerías usadas
No se añadieron ni actualizaron dependencias.

# Archivos importantes modificados
- `internal/tools/args_optional.go`
- `internal/tools/args_optional_test.go`
- `internal/tools/ssh_remote.go`
- `internal/tools/ssh_remote_test.go`
- `internal/tools/gitzip.go`

# Problemas encontrados
- `boolArg` estaba redeclarado con una firma diferente.
- `intArg` estaba redeclarado dentro del mismo paquete.
- Las llamadas de GitZip esperaban una variante de `boolArg` con valor predeterminado, pero Go resolvía la función heredada de dos parámetros.
- El helper heredado `intArg` sólo acepta enteros positivos, por lo que no era un reemplazo válido para compresión nivel cero o timeouts negativos.

# Soluciones implementadas
- Se eliminaron las declaraciones duplicadas.
- Se incorporaron `boolArgOr` e `intArgOr` como helpers compartidos y sin colisiones.
- Se actualizaron todas las llamadas de SSH y GitZip.
- Se ajustó la prueba de perfiles SSH y se añadieron regresiones para valores explícitos y defaults.

# Pendientes
- Ejecutar `test.cmd -Vet` y `go build -tags=grammar_set_core ./cmd/li` con Go 1.25.12 y dependencias oficiales en Windows.
- Probar las operaciones SSH/GitZip contra un servidor controlado.

# Próximos pasos
1. Reemplazar el proyecto local con el ZIP corregido.
2. Ejecutar la compilación y suite oficial.
3. Reportar cualquier siguiente error con su salida completa antes de publicar `v0.3.0`.
