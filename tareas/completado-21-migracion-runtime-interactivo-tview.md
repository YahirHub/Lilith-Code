# Completado 21 · Migración del runtime interactivo a tview

- [x] Incorporar `github.com/rivo/tview v0.42.0`.
- [x] Hacer que `tview.Application` controle el terminal en todos los sistemas.
- [x] Conservar la pantalla terminfo/VT explícita en Windows.
- [x] Crear una primitiva tview que pinte la vista ANSI existente sobre Tcell.
- [x] Mover teclado, pegado, ratón, resize y redraw al event loop de tview.
- [x] Entregar cada pegado como un único bloque atómico.
- [x] Preservar espacios y pegados multilinea superiores a 5,000 runes.
- [x] Evitar que una cola llena bloquee la captura física del terminal.
- [x] Conservar la semántica de Ctrl+C de Lilith y evitar el cierre automático.
- [x] Liberar correctamente la captura de mouse después de cada pulsación.
- [x] Mantener el diseño actual sin rediseño visual involuntario.
- [x] Mantener temporalmente los modelos Charm como capa de compatibilidad.
- [x] Añadir pruebas de regresión del adaptador tview.
- [x] Documentar arquitectura, alcance, riesgos y pruebas manuales.
