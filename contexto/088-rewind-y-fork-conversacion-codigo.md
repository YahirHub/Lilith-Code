# 088 — Rewind y fork de conversación y código

## Objetivo

Añadir recuperación de estado al estilo de Claude Code/OpenClaude sin depender únicamente de commits manuales del usuario:

- `/rewind`: escoger un punto anterior de la conversación y restaurar código, conversación o ambos.
- `/fork [título]`: crear una línea temporal y un workspace independientes a partir del estado actual.

## Referencia analizada

Se revisó el proyecto `openclaude-main` entregado por el usuario, en particular su selector de mensajes, el flujo `rewindConversationTo` y `forkSession`. Se conservaron sus decisiones útiles:

- seleccionar un mensaje de usuario como frontera;
- ofrecer restauración combinada, sólo conversación o sólo código;
- devolver el mensaje seleccionado al editor tras recortar la conversación;
- crear un fork con identificador nuevo, estado desacoplado y procedencia del origen;
- no copiar el historial de deshacer al fork.

Lilith amplía el fork de referencia: además de duplicar la conversación, materializa una copia/worktree del workspace porque el usuario lo solicitó explícitamente.

## Arquitectura

Se creó `internal/rewind/`:

- `Store`: persiste checkpoints comprimidos por proyecto y sesión.
- `Meta`/`Point`: metadata para el selector, conversación exacta y snapshot opcional del workspace.
- backend Git: índice temporal, `write-tree`, commit interno y ref `refs/lilith/rewind/...`.
- backend fallback: manifiesto de archivos y blobs SHA-256 deduplicados.

Los datos viven bajo `<configDir>/rewind`. Se conservan hasta 80 puntos por sesión.

## Fronteras y captura perezosa

Antes de una acción nueva del usuario se guarda una copia profunda de:

- historial del proveedor;
- transcript visual;
- Todo, Plan y Goal;
- compactaciones y metadata de sesión.

El código no se escanea en cada turno de lectura. El snapshot se adjunta al checkpoint inmediatamente antes de la primera operación potencialmente mutante:

- herramientas con `Mutating=true`;
- shell, Agent/Task y subagentes;
- herramientas MCP sin `readOnlyHint`;
- hooks `UserPromptSubmit`, `PreToolUse` o `PostToolUse`, porque son procesos externos y podrían escribir.

La captura se intenta una sola vez por turno. Si falla, el turno continúa y `/rewind` conserva la opción de restaurar sólo la conversación.

## `/rewind`

El comando se rechaza mientras haya un turno del proveedor, un comando directo o un subagente background en ejecución, evitando restaurar archivos mientras otro proceso todavía puede escribirlos. El selector muestra los checkpoints de la sesión, del más antiguo al más reciente, con estado de código completo, parcial o no disponible. Después de elegir uno ofrece:

1. Restaurar código y conversación.
2. Restaurar sólo conversación.
3. Restaurar sólo código.
4. Cancelar.

Antes de aplicar cualquier restauración se crea `Estado antes de rewind`, incluyendo el workspace actual. Esto hace reversible un rewind accidental.

Al restaurar conversación:

- se mantiene el ID de la sesión activa;
- se reemplazan historial, transcript y estados persistentes por el checkpoint;
- el mensaje del usuario seleccionado vuelve al textarea, salvo puntos internos de seguridad;
- se persiste la nueva cabeza de la línea temporal.

## Snapshots Git

La captura Git no usa `git add` sobre el índice real:

1. localiza la raíz del repo;
2. crea un `GIT_INDEX_FILE` temporal y lo inicializa desde el índice real/HEAD;
3. añade el working tree al índice temporal;
4. crea árbol y commit interno con HEAD como padre cuando existe;
5. fija el commit con una ref privada de Lilith.

La restauración usa otro índice temporal, limita `ls-tree`, `ls-files` y `checkout-index` al subdirectorio del proyecto activo, elimina dentro de ese scope los archivos tracked/untracked no ignorados ausentes en el snapshot y materializa el resto. El staging real queda sin cambios y un rewind ejecutado desde un subproyecto de monorepo no revierte cambios de directorios hermanos.

Limitación deliberada: archivos ignorados y no tracked —normalmente artefactos generados— no se capturan ni se eliminan. Los archivos ignorados que ya son tracked sí quedan incluidos.

## Snapshot fallback

Para proyectos sin Git:

- blobs por SHA-256;
- archivos regulares y symlinks;
- máximo 32 MiB por archivo y 512 MiB por snapshot;
- exclusión de `.git`, `.lilith`, `.cache`, `node_modules`, `.next`, `dist`, `build` y `target`.

Los omitidos se registran en `Skipped`, por lo que la UI muestra “código parcial” y no promete una restauración exacta.

## `/fork`

`/fork [título opcional]` sólo se ejecuta con el agente, comandos directos y subagentes background inactivos, y con una conversación no vacía. El flujo:

1. crea un punto de seguridad del estado actual;
2. captura el workspace;
3. materializa un `git worktree --detach` en Git o una copia por blobs fuera de Git;
4. genera una sesión con ID nuevo, timestamps nuevos, revisión cero y `ForkedFrom`;
5. guarda la sesión, cambia el directorio activo al fork y reconecta MCP.

El fork no comparte slices ni estados mutables con la conversación origen y no copia sus checkpoints. Si la copia se crea pero falla el guardado de sesión, se informa la ruta huérfana para no perder los archivos.

El alias antiguo `/fork` de `/subtask` fue retirado; la delegación continúa disponible mediante `/subtask`.

## Pruebas

Se añadieron pruebas para:

- crear, listar, cargar, podar y eliminar puntos;
- capturar/restaurar Git con tracked, staged, tracked-ignored y untracked;
- confirmar que el índice real no cambia;
- reemplazar correctamente directorio por archivo;
- crear worktree desde el snapshot y conservar HEAD como padre;
- restaurar un subproyecto Git sin tocar cambios tracked ni untracked de directorios hermanos;
- fallback por blobs, exclusiones y copia independiente;
- fork profundo de sesión y metadata de procedencia;
- registro separado de `/rewind`, `/fork` y `/subtask`;
- restauración de conversación, prompt y puntos de seguridad;
- no reintentar indefinidamente una captura fallida;
- política de herramientas mutantes.

## Prueba manual recomendada

1. Iniciar un turno que modifique uno o más archivos.
2. Ejecutar `/rewind`, seleccionar el prompt y probar cada modo de restauración.
3. Confirmar que el prompt vuelve al editor y que el staging Git no cambia.
4. Volver a `/rewind` y restaurar `Estado antes de rewind`.
5. Ejecutar `/fork alternativa`, confirmar la nueva ruta, modificar un archivo y comprobar que el proyecto original no cambia.
6. Repetir en un directorio sin Git para validar el backend de blobs.
