# 111 · Runner Linux y manifiestos Go ordenados

# Fecha

2026-08-04

# Objetivo

Corregir el fallo de `go mod tidy -diff` del workflow y reducir el consumo de GitHub Actions ejecutando validación, compilación multiplataforma y publicación desde un único runner Linux.

# Decisiones tomadas

- Aplicar en el repositorio el resultado canónico mostrado por `go mod tidy -diff`.
- Eliminar de `go.mod` `github.com/mattn/go-sixel` y `github.com/soniakeys/quant`, porque ningún paquete del módulo los importa actualmente.
- Versionar el `go.sum` completo producido por `go mod tidy`, incluidos hashes históricos de archivos `go.mod` requeridos por el grafo.
- Conservar `go mod tidy -diff` en CI como verificación de que los manifiestos siguen ordenados.
- Sustituir los jobs Windows + Linux por un solo job `ubuntu-latest`.
- Mantener los binarios Windows generados desde Linux mediante `GOOS`, `GOARCH` y `CGO_ENABLED=0`.
- Compilar tests sensibles para `windows/amd64` con `go test -c`, sin ejecutarlos en Linux.
- Ejecutar el smoke test del instalador PowerShell bajo `pwsh` en Ubuntu.
- Documentar explícitamente que PowerShell 5.1, CMD y el runtime Windows real requieren validación local cuando el cambio dependa de ellos.

# Arquitectura actual

```text
workflow_dispatch
  └─ release (ubuntu-latest)
       ├─ go mod tidy -diff
       ├─ versión y protección contra tags duplicados
       ├─ go test ./...
       ├─ race del orquestador
       ├─ regresión de cancelación anidada
       ├─ go vet
       ├─ go test -c para paquetes Windows sensibles
       ├─ go mod verify
       ├─ build estático Linux
       ├─ smoke tests de install.sh e install.ps1
       ├─ cmd/build: Linux + Windows
       ├─ SHA-256 y notas
       └─ GitHub Release
```

# Librerías usadas

No se añadieron dependencias. Se eliminaron dos requisitos indirectos sin uso del manifiesto.

# Archivos importantes modificados

- `go.mod`
- `go.sum`
- `.github/workflows/release.yml`
- `.github/scripts/test_install_ps1.ps1`
- `cmd/build/main_test.go`
- `install.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `contexto/111-runner-linux-y-go-mod-tidy.md`
- `tareas/completado-28-runner-linux-y-modulos-tidy.md`

# Problemas encontrados

- `go.mod` conservaba dos requisitos indirectos que `go mod tidy` elimina.
- `go.sum` sólo contenía un subconjunto manual de hashes, mientras `tidy` necesitaba registrar entradas adicionales del grafo de módulos.
- El workflow ejecutaba un job Windows completo y otro Linux, duplicando checkout, configuración de Go, descarga de módulos y parte de las pruebas.
- El smoke test PowerShell invocaba `powershell.exe` directamente y no podía ejecutarse bajo `pwsh` en Linux.

# Soluciones implementadas

- Se sincronizaron `go.mod` y `go.sum` con el diff real producido por Go 1.24 en el runner.
- Se consolidó toda la publicación en un solo job Ubuntu.
- El builder existente continúa produciendo cinco artefactos con `CGO_ENABLED=0`, incluidos Windows amd64 y arm64.
- Se añadió compilación cruzada de los tests de herramientas, shell, subagentes y TUI para detectar incompatibilidades de compilación Windows.
- El test de `install.ps1` ahora ejecuta el script con el host PowerShell actual, por lo que funciona tanto en Windows PowerShell/pwsh como en `pwsh` sobre Ubuntu.
- Se añadió una prueba del catálogo de targets para impedir que desaparezcan los artefactos Windows.

# Pendientes

- Ejecutar el workflow real para confirmar que `go mod tidy -diff` queda limpio con Go 1.24 y que `pwsh` está disponible en `ubuntu-latest`.
- Mantener `test.cmd` como validación nativa antes de publicar cambios que afecten PowerShell, CMD, Unicode o procesos Windows.

# Próximos pasos

1. Ejecutar `test.cmd` localmente para confirmar que el cambio de manifiestos no altera la suite.
2. Cambiar la versión cuando corresponda.
3. Ejecutar manualmente **Publicar release**.
4. Confirmar que el job único genera los cinco binarios y `SHA256SUMS.txt`.
