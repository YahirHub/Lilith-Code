# 108 · Integridad de módulos y tests reproducibles en Windows

# Fecha

2026-08-04

# Objetivo

Corregir la ausencia de checksums de `github.com/odvcencio/gotreesitter v0.48.0`, evitar descargas innecesarias del grafo completo y permitir ejecutar la suite desde CMD sin desactivar la verificación criptográfica cuando el DNS no resuelve `sum.golang.org`.

# Decisiones tomadas

- Versionar en `go.sum` tanto el checksum del contenido como el de `go.mod` de toda dependencia directa nueva.
- Reemplazar `go mod download all` por `go mod tidy -diff` como gate de consistencia.
- Dejar que `go test -mod=readonly` descargue sólo los módulos requeridos por la compilación y ejecutar `go mod verify` después.
- Mantener activa la base de checksums de Go en todo momento.
- Usar `sum.golang.google.cn` únicamente como alias temporal reconocido por Go cuando `sum.golang.org` no resuelve; no persistir el cambio en `go env`.
- No bloquear la suite sólo por un fallo DNS: si ambos nombres fallan, continuar con los hashes ya versionados y dejar que Go reporte únicamente una descarga que realmente necesite consultar la base.

# Arquitectura actual

```text
Repositorio
  ├─ go.mod
  ├─ go.sum completo
  ├─ test.cmd → test.ps1
  │               ├─ diagnóstico DNS
  │               ├─ go mod tidy -diff
  │               ├─ go test -mod=readonly
  │               └─ go mod verify
  └─ GitHub Actions
                  ├─ go mod tidy -diff
                  ├─ tests/vet/build
                  └─ go mod verify
```

# Librerías usadas

No se añadieron dependencias. Se mantiene `github.com/odvcencio/gotreesitter v0.48.0` y se utilizan PowerShell/.NET y herramientas oficiales de Go.

# Archivos importantes modificados

- `go.sum`
- `.github/workflows/release.yml`
- `test.cmd`
- `test.ps1`
- `README.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `contexto/105-runner-dependencias-y-posix-windows.md`
- `tareas/completado-25-corregir-integridad-modulos-y-tests.md`

# Problemas encontrados

- `gotreesitter` estaba declarado en `go.mod`, pero no tenía ninguna de sus dos entradas en `go.sum`; por eso `-mod=readonly` rechazaba `internal/codeintel`.
- `go mod download all` intentaba verificar `golang.org/x/mod` y `golang.org/x/tools`, aunque Lilith no los importa directamente para compilar la suite indicada.
- El equipo Windows no podía resolver `sum.golang.org`, por lo que cualquier módulo no presente en caché fallaba antes de las pruebas.
- La documentación afirmaba incorrectamente que descargar `all` reparaba el workspace del runner; un job readonly no debe depender de mutar `go.sum` durante CI.

# Soluciones implementadas

- Se añadieron los checksums publicados para `gotreesitter v0.48.0`.
- CI ahora falla temprano si `go mod tidy -diff` detecta cambios pendientes y ya no descarga el grafo completo.
- `test.cmd` permite ejecutar la suite desde CMD y delega en un helper PowerShell compatible con Windows PowerShell 5.1.
- El helper comprueba DNS, conserva `GOSUMDB` y sólo cambia la variable dentro de su proceso cuando puede usar el alias reconocido. Si ambos dominios fallan, continúa con `go.sum` en vez de bloquear pruebas que no necesitan una consulta nueva.
- Se documentó el comando directo para Linux/macOS o entornos donde no se necesite el helper.

# Pendientes

- Ejecutar `test.cmd -Vet` en el equipo Windows con Go 1.24 o posterior y acceso al proxy de módulos.
- Confirmar el workflow en GitHub Actions, donde sí existe conectividad a los servicios oficiales de Go.

# Próximos pasos

1. Reemplazar el proyecto local por el ZIP actualizado.
2. Ejecutar `test.cmd` desde CMD.
3. Si ambos dominios de checksum fallan, corregir DNS/red; no usar `GOSUMDB=off`.
