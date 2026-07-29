# Scroll sin anclajes y TodoWrite expandible

## Objetivo

El chat no debe mantener controles flotantes mientras el usuario lee historial antiguo. El espacio vertical es especialmente importante en terminales pequeñas.

## Comportamiento

- Cuando el transcript está al fondo, se muestra la zona normal de interacción: preguntas Plan pendientes, estado de plan, TodoWrite, actividad, cola, paleta, editor y status.
- En cuanto el usuario desplaza el viewport fuera del fondo, toda esa zona deja de renderizarse y el viewport ocupa el alto completo de la terminal.
- Al volver al fondo (`End` o desplazándose hasta el final), la zona de interacción reaparece con su estado intacto.
- Una pulsación destinada a escribir o enviar vuelve primero al fondo para evitar editar un input invisible.
- Las preguntas Plan pueden desplazarse fuera usando las teclas de scroll de página/media página sin perder respuestas parciales.

## TodoWrite

TodoWrite mantiene el modo compacto de hasta tres tareas alrededor de la tarea activa. Si existen más de tres tareas:

- `Ctrl+T` alterna compacto/expandido.
- Un clic dentro del bloque visible hace el mismo toggle.
- El modo expandido muestra la lista completa.
- El estado expandido es sólo de presentación; no se persiste ni altera el snapshot del agente.
- Al cambiar/restaurar sesión vuelve a compacto.

## Mouse y selección

El chat sólo solicita mouse reporting cuando existe un control inline clicable (preguntas Plan o un TodoWrite expandible). Fuera de esos casos conserva selección nativa de terminal. El atajo `Ctrl+T` siempre es la ruta de teclado para TodoWrite.

## Archivos

- `internal/tui/chat.go`
- `internal/tui/todo_widget.go`
- `internal/tui/plan_question_dock.go`
- `internal/tui/app.go`
- `internal/tui/help_screen.go`
- pruebas de layout/TodoWrite
