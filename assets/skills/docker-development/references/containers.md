# Contenedores: ciclo de vida e inspección

## Inventario

```sh
docker ps
docker ps -a
docker images
docker inspect <container>
```

## Ejecutar

`docker run` crea un contenedor nuevo. Decide explícitamente nombre, red, mounts, puertos, variables y política de restart.

```sh
docker run --name app --rm IMAGE
```

Para servicios duraderos evita `--rm` si necesitas inspeccionar el estado de salida.

## Start/stop/restart

```sh
docker start <container>
docker stop <container>
docker restart <container>
```

`restart` no reconstruye imagen ni vuelve a leer un Compose modificado. Si cambió configuración de Compose, recrea mediante Compose.

## Exec

```sh
docker exec <container> env
docker exec -it <container> sh
```

`docker exec` ejecuta un **ejecutable**. Para una cadena usa una shell existente dentro del contenedor:

```sh
docker exec <container> sh -c 'echo a && echo b'
```

No asumas que una imagen mínima contiene bash, sh, curl, ps o package managers.

## Logs

```sh
docker logs --tail=200 <container>
docker logs -f --since=10m <container>
```

Revisa el proceso PID 1 y exit code antes de reinstalar o reconstruir.

## Copiar archivos

```sh
docker cp <container>:/ruta ./destino
docker cp ./archivo <container>:/ruta
```

No uses `docker cp` como mecanismo de despliegue persistente: una recreación elimina esos cambios si no viven en volumen/imagen.
