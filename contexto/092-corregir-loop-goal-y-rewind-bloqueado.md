# 092 — Corregir loop de Goal y rewind bloqueado

## Síntomas observados

En algunos turnos autónomos el modelo llamaba varias veces a `create_goal` con el mismo objetivo. Cada llamada volvía a escribir el estado y el proveedor seguía viendo la herramienta disponible, por lo que podía quedar atrapado recreando el goal en lugar de ejecutar las tareas.

En `/rewind`, incluso la opción **Restaurar sólo conversación** creaba un punto de seguridad y capturaba todo el workspace antes de recortar el chat. En repositorios grandes, filtros Git lentos, gestores de credenciales o comandos sin límite de tiempo, la pantalla podía permanecer indefinidamente en `Restaurando…`.

## Corrección del loop de Goal

- `create_goal` sólo está disponible cuando no existe un goal o el anterior ya está completo; un goal pausado/bloqueado debe reanudarse o resolverse, no reemplazarse automáticamente.
- Una vez creado el objetivo, la superficie de herramientas conserva `get_goal` y `update_goal`, pero elimina `create_goal`, incluso si el nombre había quedado en la caché de herramientas del turno.
- Una llamada directa repetida se rechaza por disponibilidad, de modo que dos tool calls duplicados en el mismo lote no reinician el objetivo.
- `goal.Manager.Set` es idempotente cuando recibe exactamente el mismo objetivo activo: conserva fecha de creación, tokens, tiempo y progreso.
- Al completar o detener el goal, `create_goal` vuelve a estar disponible para iniciar otro objetivo.

No se añadió ningún límite artificial de pasos, tokens o duración al modo Goal. La corrección elimina la causa de repetición en vez de cortar la ejecución legítima.

## Corrección de `/rewind`

- **Sólo conversación** crea un punto de seguridad de conversación, pero ya no escanea ni captura el workspace.
- **Código** y **código + conversación** siguen capturando un punto de seguridad de archivos antes de restaurar.
- La creación del punto de seguridad, la captura y la restauración aceptan `context.Context`; incluso la espera del lock interno del store respeta cancelación. Git y el fallback por blobs comprueban cancelación durante sus recorridos.
- Los subprocessos Git usan `exec.CommandContext`, desactivan prompts de terminal y del gestor de credenciales, y terminan al cancelar el contexto.
- Cada operación recibe un identificador. Tras cancelar, cualquier resultado tardío se ignora y no puede aplicar una conversación vieja ni cambiar de pantalla.
- `Esc` o `Q` cancela desde la pantalla de trabajo y vuelve a la confirmación.
- Existe un timeout de 10 minutos como última protección frente a un proceso externo bloqueado.
- Un `panic` dentro del worker se transforma en error visible para que la TUI no quede congelada.

La cancelación puede ocurrir después de que una restauración de archivos haya comenzado. Por ello la UI advierte revisar el workspace si el modo incluía código, en vez de afirmar incorrectamente que nada cambió.

## Pruebas de regresión

Se añadieron pruebas para verificar:

- disponibilidad de `create_goal` antes, durante y después de un goal activo;
- rechazo de llamadas duplicadas y preservación del estado activo;
- eliminación de `create_goal` desde una superficie de herramientas ya cacheada;
- rewind sólo conversación sin captura de archivos;
- creación del punto de seguridad reversible para rewind de código;
- cancelación con `Esc` e ignorado de resultados tardíos;
- retorno inmediato de captura/restauración cuando el contexto ya está cancelado;
- timeout al esperar un store ocupado, sin quedar bloqueado en el mutex.

## Prueba manual recomendada

1. Iniciar Goal y comprobar que `create_goal` aparece una sola vez; las tareas siguientes deben usar herramientas de trabajo y `update_goal`.
2. Abrir `/rewind`, elegir **Restaurar sólo conversación** y confirmar que vuelve al chat sin escanear el proyecto.
3. Iniciar un rewind de código en un repositorio grande y pulsar `Esc`; debe regresar a la confirmación y no aplicar después un resultado tardío.
4. Revisar el workspace tras cancelar una restauración de código, ya que una operación que alcanzó la fase de escritura podría haber quedado parcial.
