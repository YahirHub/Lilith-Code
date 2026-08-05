# Estado
Completado

# Objetivo
Eliminar las colisiones de helpers en `internal/tools` que impedían compilar las nuevas herramientas SSH y GitZip.

# Implementación
- Se crearon `boolArgOr` e `intArgOr` en un archivo compartido.
- Se eliminaron las funciones duplicadas de `ssh_remote.go`.
- Se actualizaron SSH, GitZip y sus pruebas.
- Se validaron tests, detector de carreras, vet y compilación cruzada Windows mediante el harness local de dependencias.

# Resultado
El paquete afectado deja de declarar `boolArg`/`intArg` dos veces y conserva la semántica de valores predeterminados requerida por las nuevas herramientas.
