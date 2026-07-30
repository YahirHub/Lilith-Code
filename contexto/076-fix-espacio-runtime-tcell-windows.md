# 076 · Corrección de espacio en el runtime Tcell de Windows

## Fecha

2026-07-30

## Síntoma

Después de incorporar Tcell como backend físico de Windows, las letras y demás
teclas funcionaban, pero pulsar la barra espaciadora no insertaba ningún carácter
en el input del chat.

La corrección previa del pegado atómico y del borrado de filas fantasma seguía
funcionando; la regresión afectaba únicamente a la traducción de una tecla
ordinaria desde Tcell hacia Bubble Tea.

## Causa raíz

Tcell entrega un espacio normal como `KeyRune` con el rune `' '`. El adaptador lo
convertía a `tea.KeySpace`, pero construía el mensaje sin rellenar `Runes`.

Bubble Tea conserva ambos datos cuando analiza entrada ANSI: marca el tipo como
`KeySpace` y mantiene `Runes: []rune{' '}`. El componente `textarea` de Bubbles
inserta el contenido de `msg.Runes`; por eso un `KeySpace` con el slice vacío se
convertía en una operación sin efecto.

## Solución

La traducción del espacio ahora conserva el rune:

```go
tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}
```

No se cambia el acumulador de bracketed paste, el render por celdas ni el manejo
de Enter. El arreglo queda limitado al evento de espacio ordinario.

## Cobertura

Se añade una prueba de regresión que verifica que un `tcell.KeyRune` con `' '`:

- se traduzca como `tea.KeySpace`;
- conserve exactamente un rune de espacio;
- no se degrade a un mensaje vacío.

## Archivos

- `internal/tui/tcell_bridge.go`
- `internal/tui/tcell_bridge_test.go`
- `contexto/076-fix-espacio-runtime-tcell-windows.md`
- `tareas/completado-18-fix-espacio-runtime-tcell-windows.md`

## Pruebas manuales requeridas en Windows

1. Escribir varias palabras separadas por espacios en el chat.
2. Insertar espacios al inicio, en medio y al final del texto.
3. Borrar y volver a escribir espacios con Backspace/Delete.
4. Pegar texto multilinea y enviarlo para confirmar que el arreglo anterior no
   presenta regresiones.
5. Enviar el mensaje y escribir otro para confirmar que no regresan filas
   fantasma.
