# Linux: shell, permisos, procesos y filesystem

## No asumir Bash

- Identifica el intérprete del script por su shebang y el shell real disponible. `/bin/sh` puede ser `dash`, BusyBox `ash` u otro shell POSIX.
- Arrays, `[[ ... ]]`, process substitution y `set -o pipefail` no pertenecen al mínimo POSIX. Si son necesarios, declara `#!/usr/bin/env bash` y valida que Bash exista.
- Entrecomilla expansiones salvo que busques deliberadamente word splitting o globbing: `"$path"`, `"${items[@]}"` en Bash.
- Para una secuencia segura: `cmd1 && cmd2`; para alternativas: `cmd1 || cmd2`. Una pipeline puede ocultar el fallo de etapas previas si el shell no usa `pipefail`.

## Permisos

- Los bits `r`, `w`, `x` se evalúan para owner, group y others. En un directorio, `x` permite atravesarlo; `r` permite listar nombres; `w` permite modificar entradas según permisos y sticky bit.
- Inspecciona antes de cambiar: `id`, `ls -ld -- path`, `namei -l -- path` cuando esté disponible.
- Prefiere modos simbólicos legibles (`chmod u+x script.sh`) a cambios recursivos amplios.
- `sudo` cambia autoridad; no lo uses como arreglo genérico. Primero identifica propietario, grupo, mount flags, ACL, SELinux/AppArmor o filesystem de sólo lectura.

## Filesystem y rutas

- Linux distingue mayúsculas normalmente: `Readme.md` y `README.md` pueden ser archivos distintos.
- Usa `--` antes de rutas controladas por usuario cuando el comando lo soporte: `rm -- "$path"`.
- Evita parsear `ls`. Para recorridos robustos usa `find ... -print0` y consumidores que acepten NUL cuando existan nombres arbitrarios.
- `/proc`, `/sys` y `/dev` no son árboles de archivos ordinarios. `/tmp` puede limpiarse; una aplicación debe usar las rutas de estado/configuración de su plataforma.
- Un rename dentro del mismo filesystem suele ser atómico; entre filesystems se convierte en copia y borrado.

## Procesos y señales

- `SIGTERM` solicita cierre ordenado; `SIGKILL` no puede capturarse y debe ser último recurso.
- Un proceso en background no queda automáticamente desacoplado de la sesión. Según el caso usa el supervisor real (`systemd`, OpenRC, container runtime) en lugar de `nohup` improvisado.
- Comprueba procesos por PID/metadata, no por coincidencias vagas que puedan matar procesos ajenos.

Fuentes de referencia: manuales del sistema (`man 2 open`, `man 7 credentials`, `man 7 capabilities`, `man 8 mount`).
