# 079 · Migración del runtime interactivo a tview

> **Estado:** fase intermedia superada por `080-migracion-completa-tview-sin-charm.md`.
> Este documento se conserva como historial de la transición híbrida.

## Fecha

2026-07-31

## Objetivo

Reemplazar el propietario del terminal por `tview` sin reescribir en un solo
cambio toda la lógica estable del agente. La migración debe conservar el diseño,
las sesiones, el streaming, los comandos, la cola de solicitudes, los atajos y
las correcciones recientes de espacio y pegado multilinea.

## Alcance de esta fase

`tview` pasa a ser el runtime interactivo real en Windows y en los demás
sistemas operativos. Desde este cambio, `tview.Application` es responsable de:

- inicializar y finalizar la pantalla Tcell;
- ejecutar el bucle de eventos del terminal;
- habilitar bracketed paste;
- recibir teclado, ratón y redimensionamiento;
- serializar actualizaciones y redibujados;
- controlar el foco y la captura física del mouse;
- entregar `Ctrl+C` a Lilith sin aplicar el cierre automático de tview.

Los modelos existentes de Bubble Tea se conservan temporalmente como capa de
estado y compatibilidad. Ya no poseen stdin, stdout, la alternate screen, el
renderer físico ni el bucle de eventos. Esto permite migrar posteriormente cada
pantalla a primitivas nativas de tview sin mezclar esa reescritura amplia con el
cambio de runtime.

## Arquitectura

```text
Windows Terminal / PowerShell / CMD / terminal Unix
                         │
                         ▼
                tview.Application
        eventos · paste · mouse · redraw · screen
                         │
                         ▼
             tviewModelSurface (Primitive)
          traducción Tcell ↔ mensajes del modelo
                         │
                         ▼
              RootModel y pantallas actuales
          estado · comandos · streaming · sesiones
```

`tviewModelSurface` es una primitiva nativa de tview. Recibe la vista ANSI que
ya produce Lilith, la convierte mediante `cellbuf` y pinta cada celda sobre la
pantalla Tcell. Por ello el diseño actual permanece visualmente idéntico aunque
el framework que controla el terminal haya cambiado.

## Entrada y pegado

- `Application.EnablePaste(true)` activa el flujo de bracketed paste.
- `PasteHandler` recibe el bloque completo, normaliza `CRLF`/`CR` a `LF` y lo
  publica como un solo `tea.KeyMsg` con `Paste=true`.
- La barra espaciadora continúa conservando `Runes: []rune{' '}`.
- La cola física nunca bloquea el event loop de tview: cuando el modelo está
  ocupado dibujando, la publicación pendiente se desacopla sin dejar de drenar
  eventos del terminal.
- El límite visual de ocho filas del editor sigue separado del límite de
  contenido de 20,000 caracteres.

## Ctrl+C y cierre

`tview` usa `Ctrl+C` como cierre global predeterminado. Lilith necesita recibir
ese atajo para cancelar primero el turno o la cola y cerrar sólo cuando proceda.
El `InputCapture` devuelve un evento clonado de `Ctrl+C`; de esta forma tview no
aplica su cierre automático y el evento continúa hacia la lógica existente.
SIGINT/SIGTERM externos siguen deteniendo limpiamente la aplicación.

## Ratón

La aplicación activa o desactiva el mouse según `RootModel.wantsMouseCapture()`:

- en el chat normal permanece libre para seleccionar y copiar texto nativamente;
- en selectores, ajustes y controles clicables se habilita la captura;
- press/release/motion/wheel se traducen a mensajes compatibles;
- click y double-click sintéticos se ignoran para evitar activaciones duplicadas;
- la captura de una primitiva se libera después del release.

## Windows

Windows conserva la creación explícita de una pantalla terminfo/VT
`xterm-256color` para no caer en el backend de consola heredado. La pantalla se inicializa primero con manejo explícito de errores y después se
entrega a `Application.SetScreen` mediante un wrapper que evita un segundo
`Init`. Finalmente se habilitan paste, título y mouse sobre la pantalla activa.

## Dependencias

Se incorpora:

```text
github.com/rivo/tview v0.42.0
```

Bubble Tea, Bubbles y Lip Gloss todavía permanecen porque las pantallas actuales
usan sus modelos, widgets y estilos como capa de compatibilidad. No controlan el
terminal desde esta migración. Su retirada completa requiere portar pantalla por
pantalla y se hará únicamente después de comprobar paridad funcional.

## Cobertura añadida

- pegado atómico con normalización de saltos de línea;
- conservación del rune de espacio;
- pegado multilinea superior a 5,000 runes sin truncado;
- traducción de mouse sin clicks sintéticos duplicados;
- ejecución de comandos `tea.Batch`;
- clonación de `Ctrl+C` para preservar la cancelación de Lilith;
- cola de eventos no bloqueante cuando está llena.

## Archivos principales

- `internal/tui/tview_runtime.go`
- `internal/tui/tview_runtime_test.go`
- `internal/tui/tview_signals_unix.go`
- `internal/tui/tview_signals_windows.go`
- `internal/tui/runner_windows.go`
- `internal/tui/runner_other.go`
- `internal/tui/app.go`
- `cmd/li/main.go`
- `go.mod`
- `go.sum`
- `README.md`

## Pruebas manuales requeridas

### Windows Terminal, PowerShell y CMD

1. Abrir Lilith y confirmar que el diseño, colores y dimensiones coinciden con
   la versión anterior.
2. Escribir palabras con espacios, borrar con Backspace/Delete y mover el cursor.
3. Pegar una sola línea de más de 5,000 caracteres.
4. Pegar documentación con más de 24 líneas y líneas vacías; confirmar que el
   final permanece en el editor y llega al mensaje enviado.
5. Pegar mientras una respuesta está en streaming.
6. Usar Enter, Shift+Enter, Tab, Esc, Ctrl+C y Ctrl+Z según cada estado.
7. Abrir `/models`, `/provider`, `/config`, regresar con Esc y verificar que el
   streaming o la tarea activa no se congelen.
8. Probar scroll manual, auto-scroll, selector de texto y copia nativa.
9. Probar controles clicables y confirmar que un click sólo ejecuta una acción.
10. Salir normalmente y mediante señal, comprobando que el terminal se restaure.

### Linux

1. Repetir arranque, pegado, streaming, navegación, mouse y salida.
2. Confirmar recepción de SIGTERM y restauración de la terminal.

## Riesgos y siguiente fase

La capa visual todavía produce strings ANSI con Lip Gloss y usa modelos de
Bubbles. El runtime ya es tview, pero la eliminación definitiva de las
bibliotecas Charm requiere sustituir gradualmente textarea, viewport, listas,
formularios y modales por primitivas nativas. Hacerlo en fases permite comparar
cada pantalla y evita una regresión masiva en el agente.
