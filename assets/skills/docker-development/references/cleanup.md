# Limpieza Docker sin pérdida accidental

## Primero inventario

```sh
docker ps -a
docker images
docker volume ls
docker network ls
docker system df
```

## Eliminar contenedores/imágenes concretos

Preferir targets explícitos:

```sh
docker rm <container>
docker rmi <image>
```

Si el contenedor está en ejecución, decide si debe detenerse antes; no uses `-f` por defecto.

## Prune

Los comandos `prune` eliminan objetos no usados; su alcance puede ser mayor de lo que parece. Empieza por el tipo concreto:

```sh
docker container prune
docker image prune
docker network prune
```

`docker system prune -a --volumes` es una operación destructiva amplia. No la ejecutes como receta estándar y nunca sin una petición explícita que acepte la pérdida potencial de imágenes/volúmenes no usados.

## Compose

```sh
docker compose down
```

No agregues `-v` salvo que el usuario quiera eliminar datos persistentes del proyecto.

## Recuperación

Un contenedor eliminado puede recrearse desde imagen/config; un volumen eliminado puede contener datos irreemplazables. Trata volumes como el objeto más sensible del cleanup.
