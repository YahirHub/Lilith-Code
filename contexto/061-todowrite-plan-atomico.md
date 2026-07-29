# 061 · TodoWrite atómico y persistente

Fecha: 2026-07-28

## Objetivo

Incorporar a Lilith un plan de tareas propiedad del agente tomando como referencia
la extensión `todo` incluida en `pi-extensions`, sin introducir el runtime de
extensiones de Pi ni un administrador manual de tareas paralelo al chat.

## Qué se reutiliza de Pi

La extensión de Pi aporta una semántica más robusta que una lista booleana
tradicional:

- una llamada reemplaza de forma atómica el plan autoritativo completo;
- claves estables por tarea;
- estados `pending`, `in_progress` y `completed`;
- actualización dispersa de tareas existentes: campos omitidos se heredan;
- omitir una clave elimina esa tarea;
- `description: ""` y `dependsOn: []` limpian explícitamente esos campos;
- dependencias por clave con validación de referencias y ciclos;
- una tarea no puede empezar ni completarse con prerequisitos pendientes;
- `baseRevision` permite rechazar escrituras obsoletas;
- máximo de 50 tareas y 20 dependencias por tarea;
- validación all-or-nothing;
- varias tareas independientes pueden estar `in_progress`;
- las completadas se limpian al inicio del siguiente turno salvo que sigan
  siendo prerequisito de trabajo pendiente;
- widget de sólo lectura encima del editor, con una vista compacta de hasta tres
  tareas alrededor de la primera activa.

La idea útil de Codewolf que se conserva es más simple: el agente es quien
mantiene el plan y debe actualizarlo a medida que termina trabajo real y
verificación. No se agrega CRUD manual ni comandos slash específicos de todos.

## Herramienta

Lilith registra un único schema como:

```text
todo_write
```

El runtime acepta además nombres comunes emitidos por modelos entrenados con
otros agentes, sin duplicar schemas en el prompt:

```text
TodoWrite
todowrite
write_todos
todo
```

Una llamada contiene la lista completa de claves que deben permanecer. Las
tareas existentes pueden enviar sólo los campos modificados. Una tarea nueva
debe incluir al menos `key`, `subject` y `status`.

`todo_write` queda disponible para solicitudes sustantivas junto a
`tool_search`; los saludos/conversación directa continúan sin schemas de tools.
El prompt indica al modelo que lo use cuando el trabajo tenga claramente tres o
más pasos significativos, no para consultas triviales.

## Estado y persistencia

El estado vive en `internal/todo` y es protegido por mutex porque una tool puede
ejecutarse en un `tea.Cmd` mientras la TUI renderiza el widget.

Se persiste en dos lugares existentes de Lilith:

- snapshot estable de `Session`;
- `LiveCheckpoint` durante un turno activo.

Al reanudar una sesión se restaura el plan más reciente válido. Un estado
corrupto no rompe la sesión: se descarta y se inicia un plan vacío.

A diferencia de Pi, Lilith no necesita copiar sus entradas custom para
`/tree`, `/reload` o compactación. Antes de cada request al proveedor se añade
un bloque de sistema con la revisión y snapshot actual:

```text
<todo_state revision="N">
[in_progress] implement: Implementar cambio — detalle <- inspect
...
</todo_state>
```

Esto mantiene al modelo sincronizado aunque el tool result histórico sea viejo
o la sesión se haya reanudado.

## Limpieza entre turnos

Las tareas completadas permanecen visibles durante el turno en el que se
completaron. Cuando empieza el siguiente turno del usuario, se eliminan
atómicamente las completadas que ya no son prerequisito directo de tareas
`pending`/`in_progress`.

Si una tarea pendiente todavía depende de una completada, el prerequisito se
conserva. Las referencias históricas a completadas que sí se eliminan se podan
para mantener el snapshot autocontenido.

La limpieza sólo ocurre entre turnos de usuario; nunca entre una tool y la
continuación del modelo dentro del mismo turno.

## TUI

El widget se renderiza por encima del input y forma parte de
`bottomChromeHeight`, por lo que el viewport del transcript reserva su altura y
no se superpone con el editor.

Se usan marcadores ASCII para respetar el estándar visual actual de Lilith:

```text
[ ] pendiente
[>] en progreso
[x] completada
```

La vista compacta enseña como máximo tres tareas. Si existen dependencias se
muestran números de orden visual (`#2 <- #1`) y nunca las claves internas de las
tareas. Los tool results exitosos de `todo_write` no se duplican en el
transcript porque el widget ya muestra el estado final; los errores sí se
muestran.

## Diferencias deliberadas respecto a la extensión de Pi

No se copia:

- el sistema de extensiones/eventos de Pi;
- mensajes custom específicos de branch/tree/compaction;
- `/99settings`;
- render incremental de argumentos parciales mientras aún se transmite una
  llamada `todo`;
- edición manual del plan por parte del usuario.

Esas piezas dependen de APIs propias de Pi o duplicarían infraestructura que
Lilith ya posee. El núcleo útil queda implementado de forma nativa en el runtime
de sesiones, tools y TUI de Lilith.

## Validación

Las pruebas cubren:

- creación y handoff atómico;
- herencia de campos omitidos;
- borrado por omisión y limpieza explícita de campos;
- revisiones obsoletas;
- ciclos y dependencias no resueltas;
- restauración de estado persistido;
- limpieza automática de completadas entre turnos;
- aliases de compatibilidad;
- selección dinámica de `todo_write`;
- persistencia en sesión y live checkpoint;
- widget ASCII y checkpoint exacto en el system prompt.
