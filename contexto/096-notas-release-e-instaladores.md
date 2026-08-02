# 096 — Notas inteligentes de release e instaladores

## Objetivo

Publicar cada versión manual con un resumen útil de los commits incluidos y
permitir instalar o actualizar `li` sin copiar binarios ni configurar el PATH a
mano.

## Release

El workflow `Publicar release` conserva la versión centralizada en
`internal/version/version.go`. Después de probar y compilar:

1. detecta el tag SemVer anterior;
2. obtiene los commits entre ese tag y `HEAD`;
3. los agrupa como mejoras, correcciones, documentación, mantenimiento u otros;
4. enlaza cada commit y la comparación completa;
5. adjunta binarios, instaladores, notas y `SHA256SUMS.txt`.

El generador determinista está en `.github/scripts/release_notes.py`. No depende
de servicios externos ni inventa cambios: sólo resume los asuntos reales de Git.

## Instaladores

- `install.sh`: Linux AMD64, ARM64 y ARMv7. Prefiere `/usr/local/bin`, usa `sudo`
  cuando está disponible y, sin privilegios, instala únicamente en un directorio
  escribible que ya pertenezca al PATH. No modifica `.bashrc` ni requiere
  `source`. Verifica SHA-256 y reemplaza `li` de forma atómica.
- `install.ps1`: Windows AMD64/ARM64, instala en LocalAppData y actualiza el PATH
  de usuario y de la sesión PowerShell actual.
- `install.cmd`: puente para CMD que descarga y ejecuta el instalador PowerShell y
  hace visible `li` en esa sesión.

Reejecutar cualquiera de los instaladores actualiza el binario sin tocar `~/.li`.
Una versión concreta puede seleccionarse por argumento o `LI_VERSION`.

## Validación

```bash
sh -n install.sh
python3 -m py_compile .github/scripts/release_notes.py
go test ./...
git diff --check
```
