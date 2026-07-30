# 077 · Corrección de pegados largos en el runtime Tcell de Windows

## Fecha

2026-07-30

## Síntoma

El runtime Tcell ya conservaba espacios y agrupaba el bracketed paste en un solo
`tea.KeyMsg`, pero al pegar textos grandes únicamente llegaban alrededor de 200
caracteres al input del chat.

Los textos cortos funcionaban y el problema aparecía con mayor facilidad mientras
el chat estaba renderizando contenido o procesando actividad concurrente.

## Causa raíz

Tcell entrega un pegado como una secuencia física:

1. `EventPaste(true)`;
2. un `EventKey` por cada rune;
3. `EventPaste(false)`.

Aunque `tcellInputState` acumulaba correctamente esa secuencia, la consumía el
mismo `select` encargado de recibir frames, renderizar la pantalla y reenviar
mensajes a Bubble Tea. Durante un frame costoso ese bucle dejaba de drenar los
eventos físicos a tiempo.

Esto introducía presión en las colas finitas entre el terminal, Tcell y el runtime.
El tamaño aproximado observado no era un límite intencional del textarea: era el
resultado de bloquear temporalmente la captura mientras se dibujaba la interfaz.

## Solución

Se incorpora `bridgeTCellEvents`, un puente dedicado que:

- consume continuamente el canal físico de Tcell en una goroutine independiente;
- mantiene allí el estado completo del bracketed paste;
- acumula todos los runes sin involucrar al bucle de render;
- publica un único `tea.KeyMsg` atómico cuando recibe `EventPaste(false)`;
- puede cancelarse limpiamente mediante el canal de cierre del runtime.

El bucle principal de Windows ahora recibe mensajes Bubble Tea ya traducidos en
vez de eventos físicos Tcell. De esta manera, renderizar frames o procesar
comandos ya no interrumpe la lectura de un portapapeles grande.

## Compatibilidad preservada

- El espacio sigue conservando `Runes: []rune{' '}`.
- Los pegados continúan llegando con `Paste=true`.
- La normalización de saltos de línea permanece intacta.
- Resize, mouse, Enter y atajos siguen pasando por el mismo traductor.
- Bubble Tea continúa siendo responsable del modelo y los comandos.
- Tcell continúa siendo el único propietario de la consola física en Windows.

## Cobertura

Se añade una prueba de regresión con un pegado de 5,000 caracteres. La salida del
puente se mantiene intencionalmente bloqueada durante la captura para demostrar
que:

- el productor puede entregar la secuencia completa sin depender del render;
- no se publica un mensaje por cada rune;
- el resultado contiene exactamente los 5,000 caracteres;
- todo el texto se entrega en un único `tea.KeyMsg` con `Paste=true`.

## Archivos

- `internal/tui/runner_windows.go`
- `internal/tui/tcell_bridge.go`
- `internal/tui/tcell_bridge_test.go`
- `contexto/077-fix-pegado-largo-runtime-tcell-windows.md`
- `tareas/completado-19-fix-pegado-largo-runtime-tcell-windows.md`

## Pruebas manuales requeridas en Windows

1. Pegar un texto de al menos 5,000 caracteres en un input vacío.
2. Confirmar que el inicio, una sección central y el final aparecen completos.
3. Pegar texto multilinea con espacios, tabulaciones y líneas vacías.
4. Pegar mientras existe una respuesta en streaming y comprobar que no se trunca.
5. Enviar el mensaje y escribir otro para comprobar que el input no queda dañado.
6. Verificar barra espaciadora, Enter, Backspace, Delete y atajos de teclado.
7. Probar selección nativa del terminal y el mouse en las vistas que lo requieren.
