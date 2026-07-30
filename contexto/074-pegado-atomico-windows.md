# 074 · Pegado atómico mediante la entrada TTY de Bubble Tea en Windows

## Fecha

2026-07-30

## Síntomas observados

Al pegar un bloque grande en Windows Terminal, el contenido aparecía carácter
por carácter y cada carácter provocaba una actualización completa de la TUI. El
rendimiento empeoraba conforme crecía el texto. Después de enviar podían quedar
en pantalla dos filas del input, aunque el modelo sólo conservaba un editor.

Los dos intentos anteriores actuaban sobre `textarea.Reset()`, el viewport y el
recorte visual. Se retiraron del historial porque no abordaban el origen del
flujo de eventos y el problema seguía siendo reproducible.

## Causa raíz

Bubble Tea `v1.2.4` tiene dos rutas de entrada en Windows:

1. Con `os.Stdin` directo selecciona `ReadConsoleInput`. Cada
   `KEY_EVENT_RECORD` se convierte en un `tea.KeyMsg` independiente y el campo
   `Paste` no se marca. Un bloque pegado se degrada a cientos o miles de eventos.
2. Con un TTY cuyo descriptor no es el mismo que `os.Stdin`, selecciona el
   lector ANSI. Ese lector reconoce bracketed paste y entrega el bloque como un
   único `tea.KeyMsg` con `Paste=true`.

Lilith ya trataba correctamente `KeyMsg.Paste`: normalizaba saltos e insertaba
el bloque con una sola llamada a `textarea.InsertString`. El problema era que la
ruta nativa de Windows nunca generaba ese evento atómico.

`textarea.Reset()` de Bubbles `v0.20.0` ya vacía el valor, reinicia cursor,
fila/columna y viewport. No existía evidencia de que reconstruir el componente
o truncar su render solucionara la causa.

## Solución aplicada

En Windows se añade la opción pública de Bubble Tea:

```go
tea.WithInputTTY()
```

Bubble Tea abre `CONIN$` como un handle distinto de `os.Stdin`. Por su propia
lógica interna, esto hace que use el parser ANSI en vez de `ReadConsoleInput`.
La librería también se encarga de:

- poner el handle en modo raw;
- habilitar `ENABLE_VIRTUAL_TERMINAL_INPUT`;
- activar bracketed paste;
- conservar mouse VT;
- restaurar el estado de la consola al salir.

No se mantiene código propio para manipular modos de consola ni wrappers de
`io.Reader`; se usa la API pública de la librería para reducir riesgos de
restauración, cancelación y compatibilidad futura.

Linux, macOS y el resto de sistemas conservan la ruta de entrada previa.

## Archivos

- `cmd/li/main.go`
- `contexto/074-pegado-atomico-windows.md`
- `tareas/completado-16-pegado-atomico-windows.md`

## Pruebas manuales requeridas en Windows

1. Pegar un bloque de varios miles de caracteres: debe aparecer de una sola
   vez, sin animación de escritura progresiva.
2. Pegar texto multilínea y pulsar Enter: debe enviarse como una sola solicitud.
3. Escribir otro mensaje después del envío: sólo debe existir una fila de input.
4. Abrir pantallas que usan mouse y comprobar clic, rueda y redimensionamiento.
5. Salir con `/exit` y verificar que PowerShell/cmd recuperan su comportamiento
   normal.
6. Repetir en una pestaña PowerShell y en una pestaña Command Prompt de Windows
   Terminal.
