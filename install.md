# Instalación de Lilith

Lilith publica binarios para Linux, Termux/Android ARM64 y Windows. Los
instaladores detectan la plataforma, validan el archivo con SHA-256 y actualizan
una instalación existente sin borrar la configuración guardada en `~/.li`.

## Linux

Arquitecturas: AMD64, ARM64 y ARMv7.

```bash
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh | bash
```

El instalador usa `/usr/local/bin/li` directamente o mediante `sudo`. Si no
dispone de privilegios, utiliza otro directorio escribible que ya pertenezca al
`PATH`. Nunca depende de modificar `.bashrc`, por lo que `li` queda disponible
en la terminal actual sin ejecutar `source ~/.bashrc`. Si no existe ningún
destino seguro en el PATH y tampoco hay `sudo`, el instalador se detiene antes
de realizar una instalación parcial.

Para instalar o volver a instalar una versión concreta:

```bash
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh -o install.sh
sh install.sh 0.2.0
rm install.sh
```

También se puede definir `LI_VERSION=v0.2.0` o usar `LI_REPOSITORY` para un fork.


## Termux en Android

Compatibilidad nativa inicial: **ARM64/AArch64**, incluida la Samsung Galaxy Tab
A9+ SM-X210. El release contiene `li-termux-arm64`, compilado específicamente
con `GOOS=android`, `GOARCH=arm64` y `CGO_ENABLED=0`.

Instala primero `curl` y ejecuta el mismo instalador Unix:

```bash
pkg install -y curl
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh | sh
```

El instalador detecta Termux antes de tratarlo como Linux, instala el binario en
`$PREFIX/bin/li` y agrega mediante `pkg` las dependencias recomendadas que falten:
`git` y `ripgrep`. `$PREFIX/bin` ya pertenece al `PATH` de Termux, por lo que
`li` queda disponible inmediatamente y no es necesario ejecutar `source
~/.bashrc` ni reiniciar la app.

Para omitir la instalación automática de paquetes auxiliares:

```bash
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh | \
  LI_SKIP_TERMUX_PACKAGES=1 sh
```

Para una versión concreta:

```bash
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh | \
  LI_VERSION=0.1.1 sh
```

La configuración y las sesiones permanecen en `$HOME/.li`. El instalador no
solicita root, no usa `sudo` y no escribe en almacenamiento compartido de
Android.

## Windows PowerShell

Arquitecturas: AMD64 y ARM64.

```powershell
irm https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.ps1 | iex
```

Se instala en `%LOCALAPPDATA%\Programs\Lilith\bin\li.exe`. El instalador agrega
la carpeta al `PATH` persistente del usuario y a la sesión actual de PowerShell.
Una instalación anterior se reemplaza de forma atómica.

Para una versión concreta:

```powershell
$env:LI_VERSION = '0.2.0'
irm https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.ps1 | iex
```

## Windows CMD

Descarga `install.cmd` desde el release y ejecútalo. El archivo descarga el
instalador PowerShell oficial, actualiza el `PATH` de la sesión de CMD y verifica
la instalación con `li version`.

```cmd
install.cmd
```

Para una versión concreta:

```cmd
install.cmd 0.2.0
```

## Actualizar

Vuelve a ejecutar el mismo instalador. Detectará la plataforma, descargará el
release solicitado y reemplazará únicamente el ejecutable. Las sesiones,
proveedores, credenciales y preferencias permanecen intactas.

```bash
li version
```

## Compilar desde el código

Requiere Go 1.24 o superior:

```bash
git clone https://github.com/YahirHub/Lilith-Code.git lilith
cd lilith
go run ./cmd/build build
```

Los binarios se generan en `dist/`, incluido `li-termux-arm64`. Al primer arranque, Lilith crea `~/.li/` con
sus preferencias, proveedores, credenciales y sesiones.
