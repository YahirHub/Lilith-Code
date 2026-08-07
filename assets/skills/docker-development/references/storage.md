# Volúmenes, bind mounts y datos

## Elegir almacenamiento

- **Volume**: datos persistentes gestionados por Docker; buena opción para DB/estado runtime.
- **Bind mount**: ruta del host visible dentro del contenedor; útil para código/config en desarrollo o integración con archivos del host.
- **tmpfs**: datos efímeros en memoria cuando no deben persistir.

## Inspección

```sh
docker volume ls
docker volume inspect <volume>
docker inspect <container>
```

## Volumes

```yaml
services:
  db:
    volumes:
      - db_data:/var/lib/db
volumes:
  db_data:
```

Nunca asumas que `docker compose down` implica borrar el volumen; `down -v` sí puede eliminarlo.

## Bind mounts

Preferir sintaxis explícita cuando el detalle importa:

```sh
docker run --mount type=bind,src=/host/path,dst=/app,readonly IMAGE
```

Los bind mounts son escribibles por defecto y pueden modificar el host. Usa `readonly` si el contenedor sólo necesita leer.

## Permisos

Un UID/GID distinto dentro del contenedor puede no escribir un mount del host/volume. Antes de usar `chmod 777`, determina:
- UID/GID efectivo;
- ownership del path;
- quién crea el directorio;
- si un entrypoint root debe preparar permisos y luego bajar privilegios.

## Backup antes de operaciones destructivas

Para datos críticos, crea/verifica un backup antes de migrar/eliminar volumes. No declares éxito sólo porque el comando de backup terminó: comprueba contenido/tamaño y, cuando sea posible, restauración en un destino temporal.

Referencias:
- https://docs.docker.com/engine/storage/
- https://docs.docker.com/engine/storage/volumes/
- https://docs.docker.com/engine/storage/bind-mounts/
