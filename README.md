# Lilith

Lilith (`li`) es una CLI agéntica escrita en Go que corre en tu terminal.
Habla con proveedores compatibles con la API de OpenAI, ejecuta herramientas
sobre tu repositorio y ofrece una TUI interactiva construida sobre tview/Tcell.

## Características

- TUI con historial, Markdown y selector de proveedores/modelos; `/models`
  aplica la selección a la siguiente petición sin reiniciar la CLI.
- Soporte multi-proveedor: OpenCode Free, **ChatGPT Codex** mediante OAuth con
  una suscripción ChatGPT Plus/Pro, o cualquier endpoint OpenAI-compatible con
  API key.
- Herramientas integradas para leer y editar archivos, buscar con ripgrep y
  ejecutar comandos en shell POSIX (BusyBox en Windows).
- Sesiones persistentes que pueden retomarse con `li --continue`.
- Transporte resiliente para VPS/SSH, con reintentos seguros, watchdog de
  inactividad y soporte para Return recibido como `Ctrl+M`.
- Sin dependencias externas obligatorias en runtime: la toolchain auxiliar se
  descarga y verifica mediante SHA-256 al primer uso.

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

Linux:

```bash
curl -fsSL https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://github.com/YahirHub/Lilith-Code/releases/latest/download/install.ps1 | iex
```

Los instaladores detectan la arquitectura, verifican SHA-256, configuran el
`PATH` y reemplazan de forma segura una versión anterior. Consulta
[`install.md`](./install.md) para instalar una versión concreta y ver todas las
plataformas compatibles.

## Releases

La versión se define en `internal/version/version.go`. Para publicar una nueva
versión, cambia `version.Current`, haz commit y ejecuta manualmente el workflow
**Publicar release** desde GitHub Actions. El workflow prueba el proyecto,
ejecuta `cmd/build`, genera checksums, adjunta los instaladores y crea notas
agrupadas de forma automática con los commits realizados desde el tag anterior.
