# Completado 17 · Runtime dual Tcell/Bubble Tea en Windows

- [x] Mantener Bubble Tea como runtime de modelos y comandos.
- [x] Incorporar Tcell como único propietario de la consola en Windows.
- [x] Forzar el backend terminfo/VT con `NewDevTty` y evitar `cScreen`.
- [x] Dejar Bubble Tea sin renderer, entrada ni signal handler propios.
- [x] Traducir teclado, modificadores, mouse y resize a mensajes Bubble Tea.
- [x] Conservar el pegado atómico y normalizar CRLF.
- [x] Convertir la vista ANSI/Lipgloss a una cuadrícula de celdas Tcell.
- [x] Limpiar el buffer lógico completo antes de cada frame.
- [x] Forzar sincronización física después de paste, Enter y resize.
- [x] Coalescer frames de streaming sin perder un redraw pendiente.
- [x] Conservar selección nativa del terminal cuando el chat no necesita mouse.
- [x] Mantener Bubble Tea nativo fuera de Windows.
- [x] Añadir pruebas del adaptador de entrada y del borrado de filas.
- [x] Documentar arquitectura, riesgos y pruebas manuales.
