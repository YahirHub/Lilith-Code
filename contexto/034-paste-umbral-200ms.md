# ADR 034 – Paste heurística: umbral 200ms

## Contexto
La heurística anti-paste de 25 ms seguía disparando múltiples turnos al
pegar en terminales sin *bracketed paste* (PowerShell clásico). Al
revisar la doc oficial de Bubble Tea v1.2.4:

- `tea.WithoutBracketedPaste` desactiva; por defecto está ENCENDIDA.
- Cuando el terminal soporta bracketed paste, llega **un solo**
  `KeyMsg{Type: KeyRunes, Paste: true}`; nuestro `if v.Paste` ya lo
  maneja bien.
- Cuando el terminal NO lo soporta (Windows conhost, PowerShell viejo,
  algunos SSH), Bubble Tea entrega cada rune como `KeyMsg` independiente
  y el `\r\n` acaba como `"enter"`. La única forma de distinguirlo de
  tecleo humano es la latencia entre eventos.

## Decisión
Subir el umbral de la heurística de **25 ms → 200 ms**. El renderer y
el pipeline de mensajes de Bubbletea pueden meter decenas de ms entre
KeyMsg del mismo paste; 200 ms sigue siendo muy por debajo del tiempo
mínimo humano entre pulsar una tecla y luego Enter (>300 ms típicos).

Se mantiene: `v.Paste` real, `shift/alt/ctrl+enter`, cola de tareas
y Ctrl+C inteligente.

## Commit sugerido
`fix(tui): ampliar umbral anti-paste a 200ms para terminales sin bracketed paste`

**Description**: en Windows/PowerShell los saltos de línea de un pegado
se disparaban como turnos independientes porque la ventana de 25 ms
entre KeyMsgs se excedía por el render. Se amplía a 200 ms.
