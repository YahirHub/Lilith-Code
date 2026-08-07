# Redes y puertos Docker

## Concepto clave

Dentro de un contenedor, `localhost` apunta al propio contenedor. Entre servicios Compose usa el nombre del servicio por DNS en una red compartida.

Ejemplo:

```text
http://db:5432
http://api:8080
```

No uses `127.0.0.1` para llegar a otro contenedor.

## Publicar puertos

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

Vincular a `127.0.0.1` limita exposición al host local. `0.0.0.0:8080:8080` expone en interfaces del host según firewall/red. No publiques un puerto que sólo necesiten otros servicios Docker.

## Inspección

```sh
docker network ls
docker network inspect <network>
docker port <container>
docker inspect <container>
```

## DNS/aliases

En redes definidas por el usuario/Compose, usa nombres/aliases estables, no IPs de contenedor que pueden cambiar al recrear.

## Host desde contenedor

Docker Desktop suele ofrecer `host.docker.internal`. En Linux Engine la estrategia depende del entorno; no hardcodees una solución de Desktop para servidores Linux sin validarla.

Referencia: https://docs.docker.com/engine/network/
