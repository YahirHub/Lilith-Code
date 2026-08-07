# Docker Compose

## Validar configuración

```sh
docker compose config
docker compose config --services
docker compose ps -a
```

`docker compose config` ayuda a detectar interpolación/estructura antes de recrear nada.

## Build y up

```sh
docker compose build servicio
docker compose up -d servicio
```

Si quieres construir y levantar:

```sh
docker compose up -d --build servicio
```

No ejecutes `down -v` como solución genérica: `-v` elimina volúmenes declarados/anónimos asociados y puede destruir datos.

## Logs y exec

```sh
docker compose logs --tail=200 servicio
docker compose logs -f servicio
docker compose exec servicio <comando>
```

## Pull y recreate

```sh
docker compose pull servicio
docker compose up -d servicio
```

Si una imagen/tag cambió, confirma con `docker compose images`/inspect qué digest está corriendo.

## depends_on y readiness

Orden de inicio no equivale a aplicación lista. Cuando un servicio depende de DB/API lista, usa healthchecks y condiciones soportadas por Compose o lógica de retry en la aplicación; no reemplaces readiness con sleeps largos arbitrarios.

## Variables

Distingue:
- interpolación del archivo Compose (`${VAR}`);
- `environment:` entregado al contenedor;
- `env_file:`;
- secretos reales.

No publiques archivos `.env` con credenciales.

## Profiles

Usa profiles cuando servicios opcionales (debug, admin, observabilidad local) no deben iniciar siempre.

## Shutdown

```sh
docker compose stop
docker compose down
```

`down` elimina contenedores/red del proyecto, no datos en volúmenes externos; revisa flags antes de usarlo en producción.
