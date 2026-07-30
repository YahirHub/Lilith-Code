# 075 · Runtime dual con Tcell como backend de terminal en Windows

## Fecha

2026-07-30

## Decisión

En Windows, Lilith deja de permitir que Bubble Tea controle directamente la
entrada y el render físico de la terminal. Se incorpora Tcell `v2.13.10` como
único propietario de la consola, mientras Bubble Tea `v1.2.4` continúa como
runtime lógico de modelos, mensajes y comandos.

El backend no se crea con `tcell.NewScreen()`: Lilith abre `CONIN$`/`CONOUT$`
con `tcell.NewDevTty()` y construye explícitamente una pantalla terminfo/VT con
`xterm-256color`. Esto evita que una variable `TERM` ausente provoque el fallback
a `cScreen`, cuyo soporte de bracketed paste en Windows es nulo.

Linux, macOS y los demás sistemas mantienen el runtime nativo de Bubble Tea.

## Motivo

La corrección de `tea.WithInputTTY()` resolvió el pegado carácter por carácter:
el contenido del portapapeles ya llegaba como un bloque. Sin embargo, el
renderer incremental de Bubble Tea seguía dejando filas antiguas del textarea
en Windows Terminal después de contraer el input y enviar el mensaje.

El estado interno no estaba duplicado: comandos como `/exit` funcionaban aunque
la pantalla mostrara dos filas. Los intentos de reconstruir el textarea,
recortar su vista o ejecutar `tea.ClearScreen` no eliminaron de forma estable el
frame físico obsoleto.

Mantener dos renderers escribiendo alternativamente sobre la misma terminal
crearía una condición todavía más frágil. Por eso Tcell controla toda la
pantalla física en Windows, incluso cuando la lógica visible corresponde a una
pantalla auxiliar de Bubble Tea.

## Arquitectura

```text
Tcell terminfo/VT Screen (Windows)
├── NewDevTty sobre CONIN$/CONOUT$
├── entrada de teclado, mouse, resize y bracketed paste
├── grid de celdas físico
└── Show/Sync
          │
          ▼
Adaptador Lilith
├── Tcell Event -> tea.Msg
├── paste completo -> un tea.KeyMsg{Paste: true}
├── RootModel.View() ANSI -> cellbuf -> celdas Tcell
└── cola latest-only para frames de streaming
          │
          ▼
Bubble Tea headless
├── RootModel
├── ChatModel y pantallas auxiliares
├── Cmd, Batch, timers y streaming
└── WithoutRenderer + WithInput(nil)
```

Bubble Tea sigue aportando la arquitectura Elm y todos los componentes Bubbles.
Tcell no reemplaza la lógica del chat ni obliga a reescribir las pantallas; sólo
reemplaza la capa que lee y pinta la consola en Windows.

## Entrada

El adaptador convierte eventos Tcell a los mensajes Bubble Tea ya esperados por
el proyecto:

- teclas normales y combinaciones Ctrl/Alt/Shift;
- flechas, Home, End, Page Up/Down y funciones F1-F20;
- mouse, rueda, arrastre y redimensionamiento;
- bracketed paste.

Durante un pegado, el backend terminfo/VT de Tcell emite delimitadores de
inicio/fin. Lilith acumula las teclas intermedias y entrega un único
`tea.KeyMsg` con `Paste=true`. Los pares CRLF se normalizan a un solo salto para
evitar líneas vacías duplicadas. No se permite el fallback al backend nativo
`cScreen`, porque ese backend no implementa bracketed paste en Windows.

## Render

Cada `RootModel.View()` se interpreta con `cellbuf`, que convierte el ANSI y los
estilos Lipgloss en una cuadrícula. Antes de cada frame se limpia el buffer
lógico completo de Tcell y se escriben las celdas actuales.

Esto garantiza que una fila que desaparece del textarea queda representada como
celdas vacías en el siguiente frame, en lugar de depender de que un renderer de
cadenas calcule correctamente cuánto contenido anterior debe borrar.

Se usa `Sync()` cuando:

- inicia el runtime;
- cambia el tamaño de la terminal;
- llega un paste;
- se pulsa Enter;
- el frame se contrae verticalmente.

Durante streaming ordinario se usa `Show()` y una cola de capacidad uno que
conserva sólo el frame más reciente para evitar retraso visual.

## Mouse y selección

El estado lógico existente decide si la terminal debe capturar el mouse:

- chat ordinario: mouse deshabilitado para conservar selección/copia nativa;
- preguntas de plan o TodoWrite expandible: mouse habilitado;
- onboarding, ajustes y selectores: mouse habilitado.

La misma decisión se usa tanto en Bubble Tea no-Windows como en Tcell Windows.

## Archivos

- `cmd/li/main.go`
- `go.mod`
- `go.sum`
- `internal/tui/app.go`
- `internal/tui/runner_other.go`
- `internal/tui/runner_windows.go`
- `internal/tui/tcell_bridge.go`
- `internal/tui/tcell_bridge_test.go`
- `contexto/075-runtime-dual-tcell-windows.md`
- `tareas/completado-17-runtime-dual-tcell-windows.md`

## Pruebas automáticas añadidas

- paste multilínea entregado como un solo `tea.KeyMsg`;
- normalización CRLF;
- traducción de Ctrl y Alt+dirección;
- borrado de filas al contraer un frame en una `SimulationScreen`;
- conservación de una sincronización física pendiente al coalescer frames;
- `Sync()` solicitado después de paste y Enter.

## Pruebas manuales requeridas en Windows

1. Pegar un bloque de varias líneas: debe aparecer inmediatamente.
2. Enviar el bloque y comprobar que el input vuelve a una única fila.
3. Escribir otro mensaje y confirmar que no aparece un placeholder o texto
   anterior encima.
4. Repetir varios ciclos de pegar/enviar sin filas fantasma.
5. Probar `/exit`, Ctrl+C, Alt+Enter, flechas, Home/End y Page Up/Down.
6. Probar rueda y clic en ajustes, onboarding y preguntas interactivas.
7. Seleccionar y copiar texto en el chat ordinario.
8. Redimensionar Windows Terminal durante streaming y después de un paste.
9. Salir y comprobar que la consola recupera cursor, mouse y modo de entrada.
