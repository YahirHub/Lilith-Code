# Completado 22 · Migración completa a tview sin Charmbracelet

- [x] Sustituir los mensajes y comandos de Bubble Tea por `internal/tui/uikit`.
- [x] Sustituir textarea, textinput y viewport de Bubbles por componentes internos.
- [x] Sustituir Lip Gloss y `x/ansi` por estilos y helpers ANSI internos.
- [x] Sustituir Glamour por el renderer Markdown interno.
- [x] Sustituir `cellbuf` por `tview.TextView` y `tview.TranslateANSI`.
- [x] Eliminar todos los imports `github.com/charmbracelet/*`.
- [x] Eliminar los módulos Charmbracelet de `go.mod` y `go.sum`.
- [x] Renombrar los usos `tea.*` restantes a `uikit.*`.
- [x] Conservar espacio, pegado atómico, pegado largo y límite visual independiente.
- [x] Añadir una prueba que impida reintroducir dependencias Charmbracelet.
- [x] Documentar arquitectura, riesgos y pruebas manuales en `contexto/080-*`.
