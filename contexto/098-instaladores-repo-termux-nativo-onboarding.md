# Instaladores desde el repositorio, Termux nativo y onboarding

## Problemas observados

- `install.ps1` fallaba en Windows PowerShell 5.1 porque
  `RuntimeInformation.OSArchitecture` podía ser `null` y el script llamaba a
  `.ToString()` sin fallback.
- Los instaladores se adjuntaban a cada release. Una corrección del script
  obligaba a publicar de nuevo los binarios aunque éstos no hubieran cambiado.
- El artefacto Android precompilado arrancaba en Termux con un argumento extra:
  Cobra interpretaba la ruta absoluta del ejecutable como subcomando y mostraba
  `unknown command ... for li`.
- El onboarding ya existía, pero se hizo explícita y comprobable la elección de
  proveedor personalizado, ChatGPT Codex o OpenCode Free.

## Implementación

- Versión elevada a `0.1.2`.
- Los comandos de instalación usan `raw.githubusercontent.com/.../main`.
  `install.sh`, `install.ps1` e `install.cmd` permanecen sólo en el repositorio
  y ya no se copian a `dist/` ni se adjuntan al release.
- `install.ps1` detecta AMD64/ARM64 con `RuntimeInformation` y fallback a
  `PROCESSOR_ARCHITEW6432`/`PROCESSOR_ARCHITECTURE`; también tolera PATH de
  usuario vacío. El workflow lo ejecuta expresamente con Windows PowerShell
  5.1 antes de publicar.
- Termux deja de consumir `li-termux-arm64`. `install.sh` instala `git`,
  `golang` y `ripgrep` con `pkg`, resuelve el tag solicitado, clona el código,
  compila `cmd/li` con Android ARM64 y reemplaza `$PREFIX/bin/li` de forma
  segura. El checkout administrado queda en
  `$HOME/.local/share/lilith/source`.
- `cmd/li` normaliza el argumento ejecutable duplicado de Android antes de
  ejecutar Cobra. La regresión tiene prueba unitaria.
- `cmd/build` vuelve a publicar únicamente Linux y Windows. Las notas de release
  apuntan a los instaladores de la rama `main`.
- El onboarding de primer arranque presenta en este orden: proveedor
  personalizado, ChatGPT Codex y continuar con OpenCode Free. Existe una prueba
  que fija esas tres rutas.

## Validación requerida

```bash
sh -n install.sh
python3 .github/scripts/test_install_sh.py
go test ./cmd/li ./cmd/build ./internal/tui
go test ./...
go vet ./...
go run ./cmd/build build
```

En Windows, el workflow ejecuta `.github/scripts/test_install_ps1.ps1` usando
Windows PowerShell 5.1. En un Termux ARM64 real debe verificarse instalación
limpia, actualización, `li version`, apertura de la TUI y persistencia de
`$HOME/.li`.
