# Completado 16 · Pegado atómico en Windows

- [x] Retirar los dos commits fallidos del viewport/textarea.
- [x] Auditar la ruta de entrada de Bubble Tea `v1.2.4`.
- [x] Identificar `ReadConsoleInput` como origen del pegado carácter por carácter.
- [x] Confirmar que Lilith ya procesa `KeyMsg.Paste` como un bloque.
- [x] Usar `tea.WithInputTTY()` para activar la ruta ANSI oficial en Windows.
- [x] Dejar la restauración del modo de consola a Bubble Tea.
- [x] Documentar las pruebas manuales de pegado, mouse y salida.
