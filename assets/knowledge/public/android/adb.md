# Android Debug Bridge: USB, Wi-Fi y comandos habituales

ADB comunica un cliente en la computadora con el servidor local de ADB y el
daemon `adbd` del dispositivo. Viene en Android SDK Platform Tools. Antes de
diagnosticar la conexión, confirma qué binario se está usando:

```text
adb version
adb devices -l
```

El estado `device` indica conexión disponible. `unauthorized` requiere
desbloquear el teléfono y aceptar su diálogo RSA; `offline` suele requerir
reconectar el transporte o reiniciar el servidor. Si hay más de un destino,
selecciona siempre uno explícitamente:

```text
adb -s SERIAL shell getprop ro.product.model
adb -d shell                 # único dispositivo físico USB
adb -e shell                 # único emulador o transporte TCP/IP
```

## Conexión por USB

1. Instala Platform Tools y, en Windows, el driver USB del fabricante cuando
   sea necesario.
2. Activa Opciones de desarrollador y Depuración USB en Android.
3. Conecta un cable de datos, desbloquea el dispositivo y acepta únicamente la
   clave RSA de una computadora confiable.
4. Ejecuta `adb devices -l`. No continúes hasta ver el estado `device`.

Reinicio acotado del servidor host si el dispositivo no aparece:

```text
adb kill-server
adb start-server
adb devices -l
```

Cambiar de cable/puerto y revisar el modo USB suele ser preferible a revocar
todas las autorizaciones. En Linux también puede hacer falta una regla `udev`
correcta para el fabricante; no uses permisos globales como `chmod 777` sobre
dispositivos USB.

## Depuración inalámbrica segura (Android 11 o posterior)

La computadora y el dispositivo deben estar en la misma red. En Android abre
Opciones de desarrollador > Depuración inalámbrica > Vincular dispositivo con
código. El puerto de vinculación y el puerto de conexión pueden ser distintos:

```text
adb pair IP:PUERTO_VINCULACION
# introducir el código mostrado por Android cuando ADB lo solicite

adb connect IP:PUERTO_CONEXION
adb devices -l
```

Normalmente mDNS conecta automáticamente después de vincular. Si no ocurre,
usa el `IP:PUERTO_CONEXION` mostrado en la pantalla principal de Depuración
inalámbrica, no el puerto temporal de pairing. Para terminar:

```text
adb disconnect IP:PUERTO_CONEXION
```

Apaga Depuración inalámbrica al terminar. En Android puedes olvidar una sola
computadora vinculada o revocar todas las autorizaciones ADB. No marques una red
pública como confiable ni expongas un puerto ADB mediante router, VPN o Internet.

## ADB por TCP/IP con USB inicial (Android 10 o anterior)

Este flujo heredado también funciona en versiones posteriores, pero requiere
primero USB y no ofrece el pairing moderno. Conecta ambos equipos a la misma red
confiable y verifica el dispositivo USB antes de habilitar el puerto:

```text
adb -d tcpip 5555
adb connect IP_DEL_DISPOSITIVO:5555
adb devices -l
```

Obtén la IP desde los ajustes Wi-Fi del dispositivo. Al terminar, desconecta el
transporte de red y, con USB conectado, devuelve `adbd` a USB:

```text
adb disconnect IP_DEL_DISPOSITIVO:5555
adb -d usb
```

No dejes `adbd` escuchando por TCP/IP en una red no confiable.

## Comandos frecuentes

```text
adb -s SERIAL get-state
adb -s SERIAL shell
adb -s SERIAL shell COMMAND
adb -s SERIAL install app.apk
adb -s SERIAL install -r app.apk
adb -s SERIAL uninstall ID_DEL_PAQUETE
adb -s SERIAL push ARCHIVO_LOCAL /sdcard/Download/
adb -s SERIAL pull /sdcard/Download/ARCHIVO DESTINO_LOCAL
adb -s SERIAL logcat
adb -s SERIAL forward tcp:6100 tcp:7100
```

`install -r` reemplaza la aplicación conservando sus datos. `uninstall` elimina
la aplicación y normalmente sus datos: no lo ejecutes sin confirmar el package
ID y el impacto. Los paths antes de `push` son del host; los paths remotos son
del filesystem Android y siguen sus permisos y restricciones.

## Diagnóstico rápido

- `adb devices -l`: identifica serial, transporte y estado.
- `adb reconnect offline`: fuerza la reconexión de destinos offline.
- `adb mdns services`: lista servicios inalámbricos descubiertos cuando la
  versión instalada lo admite.
- `adb kill-server` seguido de `adb start-server`: reinicia sólo el servidor
  local; no corrige por sí mismo drivers, cable, autorización RSA o firewall.
- Si Wi-Fi falla, confirma misma red, que no exista aislamiento entre clientes
  y que la IP/puerto actuales coincidan con los mostrados por Android.

Fuentes oficiales: Android Developers, referencia de Android Debug Bridge y
manual `adb(1)` de Android Open Source Project.
