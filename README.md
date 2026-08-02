# Lilith

Lilith (`li`) es una CLI agéntica escrita en Go que corre en tu terminal.
Habla con proveedores compatibles con la API de OpenAI, ejecuta herramientas
sobre tu repositorio y ofrece una TUI interactiva construida sobre tview/Tcell.

## Características

- TUI con historial, Markdown y selector de proveedores/modelos; `/models`
  aplica la selección a la siguiente petición sin reiniciar la CLI.
- Onboarding de primer arranque con tres rutas claras: proveedor personalizado,
  **ChatGPT Codex** mediante OAuth o continuar con los modelos gratuitos de
  OpenCode Free.
- Herramientas integradas para leer y editar archivos, buscar con ripgrep y
  ejecutar comandos en shell POSIX (BusyBox en Windows).
- Sesiones persistentes que pueden retomarse con `li --continue`.
- Transporte resiliente para VPS/SSH, con reintentos seguros, watchdog de
  inactividad y soporte para Return recibido como `Ctrl+M`.
- Compatibilidad con Termux ARM64 mediante compilación nativa en el dispositivo:
  el instalador clona el tag estable, instala Go con `pkg`, compila e instala en
  `$PREFIX/bin`.
- Sin dependencias externas obligatorias en runtime: la toolchain auxiliar se
  descarga y verifica mediante SHA-256 al primer uso; en Termux se usan los
  paquetes nativos del repositorio.

## Atajos principales

- `Enter`: enviar el mensaje o agregar steering durante una tarea.
- `Alt+Enter`: encolar un follow-up para después del trabajo actual.
- `Shift+Enter` o `Ctrl+Enter`: insertar una nueva línea.
- `Ctrl+C`: borrar todo el texto escrito en el input sin cancelar el turno ni
  eliminar mensajes ya encolados.
- `Esc`: cancelar el turno activo.
- `Alt+↑`: recuperar al editor los mensajes pendientes de la cola.
- `/exit`: cerrar Lilith de forma explícita.

## Instalación rápida

Los instaladores viven en la rama `main`, no dentro de los assets del release.
Así pueden corregirse sin publicar otra versión de los binarios.

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | bash
```

Termux ARM64:

```bash
pkg install -y curl
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

Los instaladores actualizan una versión anterior y conservan `~/.li`. Linux y
Windows descargan el binario del release y verifican SHA-256. Termux instala
`git`, `golang` y `ripgrep`, clona la versión estable y compila nativamente para
evitar incompatibilidades del ejecutable Android precompilado. Consulta
[`install.md`](./install.md) para más opciones.

## Releases

La versión se define en `internal/version/version.go`. Para publicar una nueva
versión, cambia `version.Current`, haz commit y ejecuta manualmente el workflow
**Publicar release** desde GitHub Actions. El workflow prueba el proyecto,
ejecuta `cmd/build`, genera checksums de los binarios y crea notas agrupadas con
los commits realizados desde el tag anterior. Los instaladores no se adjuntan al
release: siempre se descargan directamente desde el repositorio.
