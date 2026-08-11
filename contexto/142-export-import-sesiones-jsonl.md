# 142. Exportar e importar conversaciones portables en JSONL

## Objetivo

Permitir mover una conversación de Lilith entre equipos mediante:

```text
/export nombredechat.jsonl
/import nombredechat.jsonl
```

El archivo transporta el contexto y progreso del chat, pero no transporta la
ruta del proyecto del equipo de origen. Al importar, Lilith vincula la sesión al
directorio de trabajo actual (`cwd`) desde el que se ejecutó `/import`.

## Formato portable

Se añadió un JSONL versionado con cabecera:

- `format = lilith-chat-jsonl`;
- `version = 1`.

Cada línea posterior contiene un registro independiente de tipo `state`,
`message`, `compaction` o `transcript`. Esto mantiene el archivo inspeccionable y
permite evolucionar el formato con una versión explícita.

Se conserva:

- historial `openai.Message` protocolariamente correcto usado por el proveedor;
- transcript visual restaurable, incluidos paneles ya materializados;
- compactaciones y sus mensajes archivados;
- Todo;
- Plan;
- Goal;
- título y fechas de la conversación.

No se serializa:

- `Session.ProjectPath`;
- `ForkedFrom.ProjectPath` ni la procedencia de workspace del fork;
- el ID local de sesión como identidad reutilizable;
- `LiveCheckpoint`/sidecar live;
- rewind/worktrees ni procesos background en ejecución.

Los textos históricos y resultados de herramientas se conservan; los campos
protocolarios internos se normalizan sólo cuando sea necesario para seguir siendo
válidos al reanudar el chat. Las menciones a rutas antiguas pueden permanecer porque
forman parte del contexto real, pero no se usan para decidir el proyecto activo
después de importar.

## Exportación

`/export archivo.jsonl` toma un snapshot del estado en memoria, no sólo del último
archivo persistido. Si se omite la extensión se añade `.jsonl`.

La escritura usa un temporal en el mismo directorio, `Sync`, permisos restrictivos
y reemplazo final. Un nombre repetido actualiza explícitamente ese backup.

Se rechaza durante un turno foreground o una compactación activa para evitar
exportar una secuencia assistant/tool a medio construir.

## Importación

`/import archivo.jsonl`:

1. resuelve el archivo relativo al `cwd` actual;
2. valida formato y versión;
3. genera siempre un ID de sesión nuevo;
4. fija `ProjectPath` al `cwd` actual, ignorando cualquier vinculación del equipo
   de origen porque esa información no existe en el export;
5. guarda la nueva sesión en el historial del proyecto receptor;
6. la carga mediante `LoadSession`, restaurando transcript, compactaciones,
   Todo/Plan/Goal;
7. reinicializa code-intel y reconecta MCP para que las siguientes herramientas
   trabajen sobre el proyecto del equipo receptor.

Importar el mismo archivo dos veces no sobrescribe la primera importación.

## Arquitectura

La funcionalidad permanece dentro del módulo `core.session`.

Se añadió la capacidad opcional `moduleapi.SessionTransferController`; de este
modo el módulo no importa `internal/tui` ni accede directamente a `ChatModel`.
La serialización vive en `internal/session/portable.go` y la adaptación del estado
en memoria en `internal/tui/session_transfer.go`.

## Pruebas

Se cubre:

- round-trip JSONL con mensajes, transcript, compactaciones, Todo/Plan/Goal;
- ausencia de `projectPath`/`ForkedFrom` en el archivo;
- generación de ID nuevo al importar;
- vinculación al proyecto receptor;
- reemplazo de un backup con el mismo nombre;
- rechazo de versiones incompatibles;
- resolución de `.jsonl` y directorio actual;
- bloqueo durante un turno activo;
- propiedad modular de `/export` y `/import` por `core.session`.

En el entorno de trabajo disponible pudo ejecutarse la suite aislada de
`internal/session` con el compilador local. La suite TUI completa requiere las
dependencias/cache y Go 1.25.12 del proyecto; la validación definitiva debe
hacerse con `scripts/test.cmd` en Windows.
