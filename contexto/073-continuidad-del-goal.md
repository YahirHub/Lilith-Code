# 073 · El estado de un goal no contamina turnos posteriores

## Fecha

2026-07-30

## Problema

Un goal persistente que ya estaba `complete`, `paused` o `blocked` podía detener
una solicitud normal posterior en su primera frontera de herramientas. El
estado durable pertenecía a la sesión, pero no existía una asociación explícita
entre ese estado y el turno autónomo que realmente lo estaba ejecutando.

## Solución

Se agregó `turnGoalManaged` al estado del turno:

- sólo se activa cuando el turno comienza con un goal activo;
- también se activa si el goal se crea o reanuda durante ese mismo turno;
- `goalStopsCurrentLoop` sólo puede detener un turno asociado;
- `endTurn` y la cancelación limpian la asociación;
- un goal finalizado anteriormente no afecta solicitudes nuevas.

## Archivos

- `internal/tui/chat.go`
- `internal/tui/goal.go`
- `internal/tui/chat_goal_test.go`

## Pruebas

- Un goal completado no detiene un turno posterior.
- El turno autónomo sí se detiene cuando su propio goal cambia de estado.
- Crear un goal durante una ejecución vincula ese mismo turno.
