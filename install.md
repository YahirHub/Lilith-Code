# Instalación de Lilith

Los instaladores se descargan directamente desde la rama `main`. Esto permite
corregir `install.sh`, `install.ps1` o `install.cmd` sin volver a compilar ni
publicar los binarios. La configuración, sesiones y credenciales permanecen en
`~/.li` y no se eliminan durante una actualización.

## Linux

Arquitecturas: AMD64, ARM64 y ARMv7.

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | bash
```

El instalador descarga el binario del último release, verifica
`SHA256SUMS.txt` y lo coloca en un directorio que ya pertenece al `PATH`,
normalmente `/usr/local/bin`. No modifica `.bashrc` ni requiere ejecutar
`source ~/.bashrc`.

Versión concreta:

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh -o install.sh
sh install.sh 0.1.2
rm install.sh
```

También se puede usar `LI_VERSION=v0.1.2` o `LI_REPOSITORY` para un fork.

## Termux en Android

Compatibilidad oficial inicial: ARM64/AArch64.

```bash
pkg install -y curl
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | sh
```

En Termux no se descarga un binario Android del release. El instalador:

1. instala o actualiza `git`, `golang` y `ripgrep` mediante `pkg`;
2. detecta el último tag estable, o la versión indicada con `LI_VERSION`;
3. clona el repositorio en `$HOME/.local/share/lilith/source`;
4. compila `./cmd/li` nativamente con Go de Termux;
5. reemplaza de forma segura `$PREFIX/bin/li`.

El código fuente conservado permite repetir una actualización y facilita el
diagnóstico. Puede cambiarse su destino con `LI_TERMUX_SOURCE_DIR`.

Versión concreta:

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | \
  LI_VERSION=0.1.2 sh
```

El runtime corrige además el argumento ejecutable duplicado que Android puede
introducir al lanzar programas Go; sin esa normalización Cobra interpretaría la
ruta absoluta de `li` como un comando desconocido.

## Windows PowerShell

Arquitecturas: AMD64 y ARM64.

```powershell
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

Se instala en `%LOCALAPPDATA%\Programs\Lilith\bin\li.exe`. El script detecta la
arquitectura tanto en PowerShell 7 como en Windows PowerShell 5.1, agrega el
directorio al `PATH` persistente del usuario y también a la sesión actual.

Versión concreta:

```powershell
$env:LI_VERSION = '0.1.2'
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

## Windows CMD

Descarga el archivo directamente desde el repositorio y ejecútalo:

```cmd
curl.exe -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.cmd -o install.cmd
install.cmd
```

Versión concreta:

```cmd
install.cmd 0.1.2
```

## Primer arranque

Al ejecutar `li` por primera vez aparece un onboarding con estas opciones:

1. proveedor personalizado OpenAI-compatible;
2. ChatGPT Codex mediante OAuth;
3. continuar con los modelos gratuitos de OpenCode Free.

Después puede volver a esa pantalla con `/login`.

## Actualizar

Vuelve a ejecutar el mismo comando de instalación. Linux y Windows reemplazan
el ejecutable usando el release solicitado; Termux vuelve a clonar el tag y lo
compila nativamente.

```bash
li version
```

## Compilar manualmente

Requiere Go 1.24 o superior:

```bash
git clone https://github.com/YahirHub/Lilith-Code.git lilith
cd lilith
go run ./cmd/build build
```

`cmd/build` genera los binarios de release para Linux y Windows. Termux se
compila en el propio dispositivo mediante `install.sh`.
