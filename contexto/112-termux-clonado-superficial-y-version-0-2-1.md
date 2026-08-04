# Fecha

2026-08-04

# Objetivo

Corregir la instalación en Termux para evitar que el instalador seleccione un tag antiguo y compile una revisión sin los manifiestos Go actuales. Elevar Lilith a la versión `0.2.1` para permitir una nueva publicación.

# Decisiones tomadas

- Termux ya no consulta tags con `git ls-remote`.
- Termux no acepta ni fija una versión, tag, commit o rama concreta durante la instalación.
- El repositorio se clona mediante `git clone --depth 1 --single-branch --no-tags`.
- Git usa la rama predeterminada anunciada por el repositorio, actualmente `main`.
- Linux y Windows conservan la selección opcional de una versión de release mediante argumento o `LI_VERSION`.
- La versión central se elevó de `0.2.0` a `0.2.1`.

# Arquitectura actual

`install.sh` detecta Termux antes del flujo de descarga de releases. En Android ARM64 instala las herramientas necesarias, crea un clon superficial temporal, compila con `GOOS=android`, `GOARCH=arm64`, `CGO_ENABLED=0` y `grammar_set_core`, instala el binario y conserva el clon superficial en `~/.local/share/lilith/source`.

# Librerías usadas

No se agregaron ni actualizaron dependencias.

# Archivos importantes modificados

- `install.sh`
- `.github/scripts/test_install_sh.py`
- `internal/version/version.go`
- `install.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`

# Problemas encontrados

El instalador resolvía el tag estable más reciente. El tag `v0.2.0` apuntaba al commit `40bb505`, anterior a la incorporación completa de `gotreesitter` en `go.sum`, por lo que Termux entraba en detached HEAD y el build fallaba por checksums ausentes.

# Soluciones implementadas

- Eliminada la resolución de tags en Termux.
- Eliminado `--branch <tag>` del clon.
- Añadido clon superficial de una sola rama y sin tags.
- Añadida una prueba que rechaza `ls-remote`, `--branch` y clones sin las tres restricciones de ahorro.
- Actualizada la documentación para aclarar que Termux siempre compila la punta publicada del repositorio.

# Pendientes

- Ejecutar instalación real limpia y actualización en un dispositivo Termux ARM64 después de publicar `v0.2.1`.

# Próximos pasos

Publicar el release manual `v0.2.1`, volver a ejecutar el comando de instalación en Termux y comprobar `li version`.
