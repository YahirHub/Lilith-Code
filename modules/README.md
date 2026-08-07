# Lilith modules

Este directorio es el espacio recomendado para módulos de distribución que no
forman parte del core. El repo público no necesita contener módulos de empresa.

Para una distribución privada, añade paquetes bajo `modules/company/**` y un
archivo `internal/distribution/company.go` protegido con `//go:build company`
que los importe por side effect. Consulta `docs/modules/README.md`.

Los módulos deben depender de `internal/moduleapi`, no de `internal/tui`.
