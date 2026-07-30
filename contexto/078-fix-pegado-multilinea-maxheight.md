# 078 · Corrección del recorte de pegados multilinea por `MaxHeight`

## Fecha

2026-07-30

## Síntoma confirmado

Al pegar documentación extensa en Windows, el input mostraba únicamente el inicio
del texto. La cantidad visible parecía rondar unos 200 caracteres, pero la captura
de reproducción reveló un patrón determinista: el contenido terminaba exactamente
en la octava línea lógica, contando también las líneas vacías.

El puente Tcell entregaba correctamente el bloque pegado y el `ChatModel` recibía
un único `tea.KeyMsg` con `Paste=true`; el recorte ocurría después, al insertarlo en
el componente `textarea`.

## Causa raíz

El chat configuraba simultáneamente:

```go
ta.SetHeight(1)
ta.MaxHeight = 8
```

En Bubbles `textarea.Model`, `MaxHeight` no representa sólo la altura visual. El
método `InsertString` también lo usa como límite máximo de líneas lógicas
almacenadas. Cuando un pegado contenía más de ocho líneas, descartaba todas las
posteriores antes de que el modelo pudiera mostrarlas o enviarlas.

Por eso el texto de la captura se detenía en la línea 8 y daba la impresión de un
límite aproximado de caracteres.

## Solución

Se separan las dos responsabilidades:

- `textarea.MaxHeight = 0` deja ilimitada la cantidad de líneas lógicas, dentro
  del límite general de 20,000 caracteres del chat.
- `chatInputVisibleMaxHeight = 8` conserva el diseño: la caja sólo ocupa hasta
  ocho filas de terminal y usa el viewport interno del textarea para desplazarse.
- `setInputHeightForContent` usa el nuevo límite visual y deja de consultar
  `textarea.MaxHeight`.

El cambio no modifica el puente de Tcell, el pegado atómico, la normalización de
CRLF ni la corrección de la barra espaciadora.

## Cobertura

Se añade una prueba de regresión que pega 24 líneas con más de 1,000 runes y
verifica que:

- las 24 líneas permanezcan completas en `textarea.Value()`;
- `LineCount()` sea 24;
- la caja visible continúe limitada a 8 filas;
- `textarea.MaxHeight` quede ilimitado para que Bubbles no descarte contenido.

## Archivos

- `internal/tui/chat.go`
- `internal/tui/chat_streaming_input_test.go`
- `internal/tui/chat_layout_test.go`
- `contexto/078-fix-pegado-multilinea-maxheight.md`
- `tareas/completado-20-fix-pegado-multilinea-maxheight.md`

## Pruebas manuales requeridas en Windows

1. Pegar el mismo documento de la captura, con más de ocho líneas y líneas vacías.
2. Confirmar que el final del documento permanece en el input desplazándose dentro
   de la caja de ocho filas.
3. Enviar el mensaje y verificar que el agente recibe el inicio, una sección
   intermedia y el final.
4. Pegar una sola línea de al menos 5,000 caracteres.
5. Verificar espacio, Enter, Shift+Enter, Backspace y Delete.
6. Pegar mientras existe una respuesta en streaming y confirmar que el contenido
   no se fragmenta ni se envía antes de tiempo.
