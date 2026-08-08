# 138. Scripts de instalación y pruebas fuera del root

## Fecha

2026-08-08

## Objetivo

Reducir el ruido del directorio raíz trasladando los instaladores públicos y
helpers de validación multiplataforma a `scripts/`, sin romper su ejecución
local, las descargas desde la rama `main` ni el workflow de publicación.

## Decisiones tomadas

- `scripts/` es ahora la ubicación canónica de `install.sh`, `install.ps1`,
  `install.cmd`, `test.ps1` y `test.cmd`.
- `install.md` permanece en el root porque es documentación de entrada, no un
  ejecutable.
- No se dejan shims duplicados en el root: mantener dos rutas produciría dos
  fuentes de verdad y no resolvería el objetivo de limpieza.
- Las URLs públicas usan `main/scripts/<instalador>`; las notas de release se
  generan con esas mismas rutas.
- `scripts/test.ps1` calcula la raíz como el padre de `scripts/`; `test.cmd`
  sigue resolviendo el helper PowerShell desde su propio directorio.

## Arquitectura actual

```text
scripts/
├── install.sh
├── install.ps1
├── install.cmd
├── test.ps1
└── test.cmd
```

El workflow y los smoke tests internos consumen esa ubicación canónica. Los
instaladores continúan fuera de los assets de release: se descargan directamente
desde la rama predeterminada y luego instalan un binario publicado, salvo Termux,
que compila nativamente desde un clon superficial.

## Librerías usadas

No se añadieron ni actualizaron dependencias.

## Archivos importantes modificados

- `scripts/install.sh`, `scripts/install.ps1`, `scripts/install.cmd`;
- `scripts/test.ps1`, `scripts/test.cmd`;
- `.github/workflows/release.yml`;
- `.github/scripts/test_install_sh.py`;
- `.github/scripts/test_install_ps1.ps1`;
- `.github/scripts/release_notes.py`;
- `.gitattributes`, `README.md`, `install.md`, `AGENTS.md`;
- `cmd/build/main_test.go`;
- `contexto/000-contexto-maestro.md`.

## Problemas encontrados

- `test.ps1` usaba `$PSScriptRoot` como raíz del repositorio; después del
  traslado habría ejecutado Go desde `scripts/`.
- Instaladores, CI, smoke tests y notas de release conservaban rutas absolutas
  al root.
- El smoke test PowerShell se ejecuta también en Linux mediante `pwsh`, por lo
  que construir el path con un backslash literal no era portable.

## Soluciones implementadas

- Se calcula explícitamente el padre de `scripts/` antes de ejecutar la suite.
- Todas las referencias vigentes y URLs de descarga apuntan a `scripts/`.
- El smoke test PowerShell usa llamadas anidadas a `Join-Path`.
- `.gitattributes` conserva CRLF para los helpers Windows en su nueva ruta.

## Validación

Se validó con Go 1.25.12:

```text
sh -n scripts/install.sh
python3 .github/scripts/test_install_sh.py
go mod tidy -diff
go mod verify
go test -mod=readonly -tags=grammar_set_core ./...
go test -race -mod=readonly -tags=grammar_set_core ./...
go vet -mod=readonly -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true -mod=readonly -tags=grammar_set_core ./...
```

La simulación cubrió actualización Linux y compilación nativa Termux sin red.
También se generaron notas de release de prueba y se comprobó que sólo contienen
URLs bajo `main/scripts/`.

## Pendientes

- Ejecutar `scripts\test.cmd -Vet` en Windows PowerShell 5.1/CMD cuando se
  disponga del host nativo.
- Los comandos antiguos que descargaban desde `/main/install.*` deben migrarse
  a `/main/scripts/install.*`; no se mantienen copias legacy en el root.

## Próximos pasos

Usar siempre las rutas bajo `scripts/` en documentación, incidencias y cambios
futuros del pipeline de instalación o validación.
