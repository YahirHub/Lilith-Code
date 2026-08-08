# Termux: entorno, paquetes, storage y limitaciones

Termux ejecuta binarios Android nativos dentro del sandbox de la aplicación. No es una distribución Linux con rootfs/FHS tradicional, ni equivale a un contenedor o una VM.

## Entorno y rutas

- `$PREFIX` suele apuntar al prefijo privado de Termux, por ejemplo `/data/data/com.termux/files/usr` según instalación/usuario. Usa la variable, no codifiques la ruta.
- `$HOME` pertenece al sandbox privado de la app. Los prefijos `/usr/bin` o `/bin` de scripts Linux pueden no apuntar a los binarios Termux.
- Un shebang portable dentro de Termux puede usar `#!/data/data/com.termux/files/usr/bin/bash` sólo si controlas ese entorno; para distribución amplia, instala/adapta el script durante el empaquetado.
- `termux-exec` corrige parte de las diferencias al ejecutar programas, pero no convierte el sistema en FHS.

## Paquetes

- Usa `pkg update`, `pkg upgrade`, `pkg install <paquete>` y `pkg search <texto>` como interfaz normal.
- No mezcles repositorios de una distro Debian/Ubuntu con los de Termux. Los binarios glibc normales no son necesariamente compatibles con Android/Bionic.
- No presupongas `sudo`, `systemd`, Docker daemon, `/etc` global ni servicios de una distro de escritorio.

## Storage de Android

- `termux-setup-storage` solicita acceso y crea accesos bajo `~/storage` cuando Android y la versión de la app lo permiten.
- Shared/external storage tiene semántica y restricciones diferentes: ejecución, enlaces, permisos Unix, sockets y atributos pueden no funcionar. Mantén repositorios, entornos y ejecutables en el storage privado de Termux; copia sólo datos de intercambio al storage compartido.
- Respeta permisos de Android y Scoped Storage. Un path visible no implica acceso autorizado.

## Procesos y límites

- Android puede suspender o matar procesos en background por ahorro de batería, memoria o políticas del fabricante. Un proceso largo requiere expectativas explícitas y, cuando aplique, integración con Termux:API/Boot/WakeLock.
- Puertos privilegiados, namespaces, mounts y otras operaciones de kernel pueden requerir root o no estar disponibles.
- Antes de proponer una herramienta comprueba `command -v`, la arquitectura (`uname -m`) y el paquete Termux correspondiente.

Fuentes oficiales del proyecto: wiki de `termux/termux-packages`, “Termux execution environment” y “Termux file system layout”.
