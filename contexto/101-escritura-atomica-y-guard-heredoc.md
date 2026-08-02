# 101 — Escritura atómica y protección contra truncado de archivos

## Problema observado

Durante una auditoría real, un modelo intentó generar `reporte.md` mediante un
único heredoc largo enviado a `run_terminal_command`. La llamada llegó truncada
al shell: Bash no recibió el delimitador final y guardó sólo una parte del
documento. Una edición posterior con `str_replace` también falló porque su texto
objetivo correspondía a una versión anterior e incompleta del archivo.

El fallo no pertenecía al índice de inteligencia de código. Era una debilidad de
la superficie de escritura: una herramienta de terminal no debe transportar un
documento grande como parte de su propia línea de comandos.

## Decisión

Se incorporan herramientas explícitas de archivo y un backend común:

- `write_file`: crea contenido completo; para reemplazar exige
  `overwrite=true` y admite `expected_sha256`;
- `append_file`: agrega una sección acotada y puede crear el destino;
- `create_file`: continúa siendo estrictamente create-only;
- `str_replace` y `apply_diff`: conservan edición localizada, pero escriben a
  través del backend atómico.

No se usa Bash, PowerShell, CMD, base64 ni archivos auxiliares externos para
transportar el contenido. Las herramientas forman parte del binario estático.

## Backend atómico

Cada operación:

1. resuelve y bloquea la ruta dentro del runtime;
2. crea un temporal en el mismo directorio;
3. escribe por bloques comprobando cancelación;
4. preserva permisos cuando el destino existe;
5. sincroniza el temporal;
6. publica el archivo completo mediante rename/reemplazo nativo;
7. verifica el tamaño final y devuelve SHA-256, bytes y líneas.

En una creación estricta, Unix publica el temporal mediante hard link sin
clobber y Windows usa `MoveFileExW` sin la bandera de reemplazo. Si otro proceso
crea el destino después del preflight, Lilith falla sin sobrescribirlo. Para un
reemplazo intencional, Windows usa `MOVEFILE_REPLACE_EXISTING |
MOVEFILE_WRITE_THROUGH`; Unix usa rename dentro del mismo filesystem.

El `fsync` del directorio en Unix es best-effort después de la publicación: una
plataforma que no lo soporte no convierte una escritura ya completada en error,
porque un reintento de `append_file` podría duplicar la sección.

## Límites y uso del modelo

Una llamada individual acepta hasta 1 MiB. El archivo final construido con
`append_file` está limitado a 64 MiB. Los reportes grandes deben dividirse por
secciones semánticas completas y no por cortes arbitrarios de bytes.

Para un archivo existente:

- preferir `str_replace` o `apply_diff` en cambios de código localizados;
- usar `write_file(overwrite=true)` sólo cuando se desea regenerar el documento
  completo;
- usar el SHA devuelto por una lectura/escritura como `expected_sha256` cuando
  se encadenan operaciones;
- usar `append_file` para anexar una sección, no `cat >>`.

## Protección de terminal

`internal/shell` inspecciona el comando antes de resolver o iniciar la shell:

- un heredoc cuyo delimitador final no está presente se rechaza siempre;
- un heredoc o escritor inline de más de 6 KiB se rechaza;
- el mensaje confirma que no se ejecutó ningún comando ni se creó un archivo
  parcial, y recomienda `write_file`/`append_file`;
- comandos largos que no contienen una escritura inline siguen permitidos.

La protección cubre el caso donde el proveedor corta el JSON/argumento antes de
que Lilith reciba la llamada completa: si todavía existe un comando analizable
pero el heredoc quedó sin terminador, nunca llega a Bash.

## Diagnóstico de `str_replace`

Cuando el texto objetivo no existe o es ambiguo, el error ahora incluye:

- ruta, bytes, líneas y SHA-256 actuales;
- una ventana de líneas cercana calculada sólo como diagnóstico;
- un `retry_hint` con `read_files`, offset y limit.

Lilith no aplica automáticamente el fragmento aproximado. El agente debe leer la
región vigente y volver a enviar una edición exacta.

## TUI y selección perezosa

- `write_file` y `append_file` son herramientas reales y se muestran como
  paneles de archivo; el alias ambiguo `write` continúa bloqueado;
- el schema emite `path`, autorización/checksum y luego `content`, permitiendo
  preflight mientras los argumentos aún llegan;
- si `write_file` apunta a un destino existente sin `overwrite=true`, la TUI
  detiene el stream, vacía el cuerpo de la tool call guardada y devuelve
  `OVERWRITE_REQUIRED`;
- solicitudes sobre reportes/documentos materializan `write_file` y
  `append_file` sin exponer indiscriminadamente `create_file`.

## Pruebas de regresión

Se cubren:

- contenido Unicode exacto de 8 KiB, 64 KiB y 1 MiB;
- reemplazo autorizado y preservación de permisos;
- checksum obsoleto;
- append por secciones;
- cancelación sin mutar el destino;
- creación no-clobber;
- ausencia de temporales filtrados;
- heredoc incompleto sin archivo parcial;
- heredoc/PowerShell inline demasiado largo;
- preflight de TUI para overwrite y paneles de archivo;
- diagnóstico enriquecido de mismatch en `str_replace`;
- ejecución de `internal/tools` e `internal/shell` en el job Windows del workflow de release.
