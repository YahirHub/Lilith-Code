# Tarea 25 · Corregir integridad de módulos y ejecución de tests

## Estado

Completada y documentada.

## Objetivo

Corregir el `go.sum` incompleto introducido con `gotreesitter`, evitar que el workflow descargue módulos ajenos a las pruebas mediante `go mod download all` y ofrecer una ejecución segura de tests en Windows cuando `sum.golang.org` no resuelve por DNS.

## Criterios de aceptación

- `go.sum` contiene los checksums correspondientes a `github.com/odvcencio/gotreesitter v0.48.0` y de las dependencias realmente necesarias para compilar Lilith.
- CI valida que `go.mod` y `go.sum` estén ordenados mediante `go mod tidy -diff`.
- CI deja que `go test -mod=readonly` descargue sólo los módulos requeridos y ejecuta `go mod verify` después.
- El helper Windows conserva la verificación criptográfica y usa únicamente el alias reconocido por Go para la base de checksums cuando `sum.golang.org` no resuelve.
- No se configura `GOSUMDB=off`, `GONOSUMDB` ni una exclusión persistente.
- README, AGENTS y `contexto/` reflejan el procedimiento vigente.
