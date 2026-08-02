# Termux ARM64, agentes y skills integradas

## Objetivo

Publicar Lilith 0.1.1 con una ruta nativa y explícita para Termux, evitando que
Android se trate simplemente como una distribución Linux FHS. La versión 0.1.0
ya podía recorrer un `PATH` que incluyera `$PREFIX/bin`, pero la compatibilidad
era parcial: el release no contenía un target Android, los hooks invocaban
`/bin/sh` y la toolchain intentaba descargar artefactos Linux para `ripgrep`.

## Implementación

- `internal/version/version.go` cambia a `0.1.1`, por lo que el workflow puede
  crear el tag nuevo `v0.1.1`.
- `cmd/build` genera `li-termux-arm64` con `GOOS=android`, `GOARCH=arm64` y
  `CGO_ENABLED=0`, además de los targets Linux y Windows existentes.
- El workflow simula una instalación/actualización Termux sin red, comprueba que
  no se modifique `.bashrc` y después valida que el artefacto Termux exista y
  contenga metadata Go antes de preparar checksums y publicar el release.
- `install.sh` detecta Termux por `$PREFIX`/variables de la aplicación antes de
  seleccionar un target Linux. Sólo acepta ARM64/AArch64, instala de forma
  atómica en `$PREFIX/bin/li`, verifica SHA-256 y conserva `$HOME/.li`.
- Cuando faltan `git` o `ripgrep`, el instalador usa `pkg install -y`. Se puede
  omitir con `LI_SKIP_TERMUX_PACKAGES=1`.
- `internal/toolchain` instala `ripgrep` mediante `pkg` cuando el binario se
  ejecuta con `runtime.GOOS == android`; no descarga un build GNU/Linux.
- Los hooks resuelven `bash` o `sh` con `toolchain.ShellCommand`, eliminando la
  dependencia de `/bin/sh`.
- `code_search` muestra el comando correcto `pkg install ripgrep` en Android.

## Agentes y skills incluidas

Agentes embebidos:

- `termux-specialist`: implementa compatibilidad, instaladores y runtime.
- `termux-auditor`: auditor de sólo lectura para detectar supuestos FHS, ABI y
  dependencias incompatibles.

Skills embebidas:

- `termux-development`: paths, shell, paquetes, storage, subprocessos y TUI.
- `termux-release`: build Android ARM64, checksums, instalación y actualización.

Las definiciones se materializan desde `assets/agents` y `assets/skills` con la
misma precedencia existente: una versión de usuario o proyecto con el mismo
nombre puede reemplazar la integrada.

## Compatibilidad declarada

La compatibilidad Termux oficial de esta versión se limita a ARM64/AArch64. No
se publican targets Android 32-bit ni x86 porque con la configuración estática
actual requieren rutas de enlace diferentes; el instalador debe fallar de forma
clara en vez de entregar un binario no verificado.

Termux usa su prefijo privado y coloca `$PREFIX/bin` en `PATH`; por eso no se
modifica `.bashrc` ni se requiere `source`. Los ejecutables y dependencias que
usan paths FHS deben adaptarse al prefijo de Termux, no copiarse ciegamente
desde una distribución GNU/Linux.

## Validación requerida

```bash
gofmt -w cmd/build internal/hooks internal/toolchain internal/tools
sh -n install.sh
python3 -m py_compile .github/scripts/release_notes.py
go test ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/li-termux ./cmd/li
go run ./cmd/build build
```

En un dispositivo Termux ARM64:

1. instalación limpia desde el release;
2. ejecución inmediata de `li version` y `li` sin recargar perfil;
3. actualización sobre una versión anterior;
4. `git`, `rg`, shell, hooks, Enter, Ctrl+C, Esc, pegado y resize;
5. persistencia de `$HOME/.li` después de actualizar.
