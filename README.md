# Lilith

Lilith (`li`) es una CLI agéntica escrita en Go que corre en tu terminal.
Habla con proveedores compatibles con la API de OpenAI, ejecuta herramientas
sobre tu repositorio y ofrece una TUI cuidada construida con Bubble Tea.

## Características

- TUI con historial, markdown y selector de proveedores/modelos; `/models` aplica la selección a la siguiente petición sin reiniciar la CLI.
- Soporte multi-proveedor: OpenCode Free (gratis, incluido), suscripción
  ChatGPT/Codex vía OAuth, o cualquier endpoint OpenAI-compatible con API key.
- Herramientas integradas: lectura de ficheros, búsqueda con ripgrep,
  ejecución de comandos en shell POSIX (busybox en Windows). Ctrl+C cancela
  el turno y su árbol de procesos sin permitir que resultados tardíos lo reactiven.
- Sesiones persistentes con `li --continue`.
- Sin dependencias externas en runtime: la toolchain (ripgrep, busybox) se
  descarga y verifica por SHA-256 al primer uso.

Consulta [`install.md`](./install.md) para las instrucciones de instalación.
