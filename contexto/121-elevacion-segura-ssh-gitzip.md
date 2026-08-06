# 121 — Elevación segura para SSH y GitZip remoto

## Fecha

2026-08-05

## Objetivo

Permitir que una conexión SSH abierta con una cuenta no `root` pueda subir,
leer, modificar y desplegar archivos en rutas protegidas cuando esa cuenta tiene
acceso administrativo mediante `sudo` o `doas`, sin exponer contraseñas ni abrir
una segunda conexión.

## Problema detectado

SFTP opera siempre con el UID de la cuenta SSH y no puede anteponer `sudo` a una
subida. Por ello, una cuenta administrativa como `deploy` podía ejecutar comandos
privilegiados, pero recibía `permission denied` al subir directamente a `/opt`,
`/var/www`, `/etc` u otro destino propiedad de `root`. GitZip heredaba el mismo
problema al subir, crear o extraer archivos en el servidor.

## Diseño aplicado

- `privilege_mode` admite `auto`, `never` y `required`.
- `auto` prueba primero el acceso normal para no elevar operaciones que no lo
  necesitan. Si el servidor SFTP devuelve sólo `failure`, Lilith comprueba el
  acceso real con una operación de shell antes de decidir la elevación.
- Cuando una escritura SFTP falla por permisos, Lilith sube el contenido a una
  ruta temporal escribible y lo publica en el destino mediante:
  1. la sesión actual si su UID es `0`;
  2. `sudo -n` cuando existe una regla sin contraseña;
  3. `doas -n` cuando está disponible sin contraseña;
  4. `sudo -S` con contraseña solicitada en un popup local específico.
- Los errores locales, destinos existentes, rutas que son directorios y otras
  condiciones que sudo no puede corregir se validan antes de elevar.
- La contraseña sudo se entrega únicamente por `stdin`, nunca forma parte del
  comando, tool call, transcript, log o resultado. Se conserva sólo en memoria
  dentro de la conexión lógica y se elimina al cerrarla.
- Al reemplazar un archivo, el instalador temporal conserva propietario y modo
  existentes. Para un archivo nuevo hereda el propietario del directorio padre.
- Lectura, descarga, listado y `stat` de rutas protegidas usan una copia temporal
  legible por la cuenta SSH. Escritura, subida, `mkdir`, renombrado y borrado
  comparten la misma política. La eliminación de `/` se rechaza siempre.
- Un `exec` arbitrario nunca se repite automáticamente con sudo después de un
  error, porque el primer intento pudo aplicar efectos parciales. Para ejecutar
  todo el comando elevado se exige `privilege_mode=required` desde el inicio.

## GitZip remoto

- `upload` usa la subida temporal privilegiada cuando `remote_path` está
  protegido.
- `remote_create` comprueba antes la lectura de `source_path` y la escritura del
  archivo de salida; si alguna requiere privilegios, escanea y crea el archivo
  elevado desde el principio.
- `remote_extract` comprueba el archivo y el destino antes de extraer para no
  ejecutar parcialmente y después repetir.
- `cleanup_remote_archive`, `stat` y exploración de reglas ignore también usan la
  capa de privilegios.
- Se conserva el mismo `connection_id`; no se abre un proceso `ssh` paralelo.

## Archivos principales

- `internal/interaction/bridge.go`
- `internal/tui/permission_widget.go`
- `internal/sshremote/manager.go`
- `internal/sshremote/privilege.go`
- `internal/sshremote/privilege_test.go`
- `internal/tools/ssh_remote.go`
- `internal/tools/ssh_remote_test.go`
- `internal/tools/gitzip.go`
- `internal/tools/gitzip_test.go`
- `README.md`
- `install.md`
- `AGENTS.md`

## Pruebas requeridas

1. Con una cuenta no root y `sudo` habilitado, subir un archivo a
   `/opt/lilith-test/release.zip`; debe aparecer un único popup de contraseña
   sudo si no existe NOPASSWD.
2. Repetir otra subida con el mismo `connection_id`; no debe volver a pedir la
   contraseña sudo durante esa conexión.
3. Ejecutar la misma prueba con `privilege_mode=never`; debe devolver acceso
   denegado sin elevar.
4. Probar `write_file`, `mkdir`, `rename`, `read_file`, `download`, `list`,
   `stat` y `delete` sobre una ruta protegida.
5. Crear y extraer un GitZip remoto dentro de `/opt`; debe preflightar y elevar
   antes de iniciar el comando largo.
6. Ejecutar un comando general con `privilege_mode=required` y verificar que se
   ejecute elevado; con `auto`, un error de permisos sólo debe devolver una pista
   y no repetir el comando.
7. Confirmar que tool calls, logs y resultados no contienen la contraseña sudo.
8. Ejecutar `go test -mod=readonly -tags=grammar_set_core ./internal/sshremote ./internal/tools ./internal/tui` y el workflow completo.
