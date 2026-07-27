# 002 · Toolchain portable y shell en Windows

## Contexto

El portado necesita ejecutar comandos de shell (equivalente al agente `basher`
del original y al futuro modo `!bash` de la TUI). En Windows no existe
`/bin/sh`, y tampoco se puede asumir Git Bash, WSL ni `rg` instalados.

Requisito adicional del usuario: `original/` debe conservarse en cada entrega
del código para no perder la referencia de portado.

## Decisión

1. `internal/toolchain` — catálogo declarativo de dependencias externas con
   URL, SHA-256 y formato de archivo por `goos/goarch`, más resolución de
   ejecutables y del shell del sistema.
2. `internal/shell` — ejecución de comandos con timeout obligatorio,
   salida acotada y resultado estructurado (`stdout`, `stderr`, `exitCode`,
   `timedOut`, `durationMs`), mismo contrato que devolvía `basher.ts`.
3. `cmd/build` — comando de preparación: `go run ./cmd/build check|install`.
   Descarga por HTTPS, verifica SHA-256 y escribe en `~/.li/tools/bin` con 0700.

Herramientas del catálogo:

| Herramienta | Windows | Linux/macOS |
|-------------|---------|-------------|
| shell       | `busybox.exe` (busybox-w32) si no hay bash | `bash`/`sh` del sistema |
| `rg`        | ripgrep 14.1.1 msvc | ripgrep 14.1.1 musl/gnu/darwin |

Orden de resolución del shell: en Windows `bash` del PATH → rutas conocidas de
Git for Windows → `busybox sh`; en el resto `bash` → `sh`.

## Alternativas descartadas

- **PortableGit**: aporta un Bash completo pero pesa ~250 MB y requiere 7-Zip
  para descomprimir. Desproporcionado.
- **WSL**: no se puede exigir; requiere privilegios de administrador.
- **PowerShell como shell del agente**: obligaría a mantener dos dialectos de
  comandos y rompería la compatibilidad con los prompts portados del original.
- ripgrep para `windows/arm64`: no hay build oficial, se usa el x64 por emulación.

## Seguridad

- Sólo HTTPS; cualquier otro esquema se rechaza antes de la descarga.
- SHA-256 obligatorio y comparado antes de escribir en disco.
- Límite de 64 MB por artefacto y escritura atómica con permisos 0700.
- El shell nunca hereda stdin y siempre corre con directorio de trabajo
  validado y timeout por defecto de 30 s (salida recortada a 256 KB).
- Un exit code distinto de cero no es un error de Go: se reporta al llamador.

## Pendiente

- Conectar `internal/shell` con la TUI (`!comando`) y con las tool calls del modelo,
  incluyendo la confirmación previa del modo seguro (ver `original/docs/safe-mode.md`).

## Regla operativa

`original/` permanece en el repositorio en cada entrega. Es solo lectura: no se
compila (no contiene Go) ni se edita.