# Depuración Docker

## Orden de diagnóstico

1. `docker compose ps -a` / `docker ps -a`.
2. Exit code, health y restart count en inspect.
3. Logs recientes.
4. Configuración efectiva (`docker compose config`).
5. ENTRYPOINT/CMD y archivos montados.
6. Red/DNS/puertos.
7. Permisos/UID/GID y storage.
8. Sólo después reconstruir/cambiar arquitectura.

## Crash loop

```sh
docker inspect <container>
docker logs --tail=300 <container>
```

Si una política de restart lo reinicia demasiado rápido, puedes detener temporalmente el servicio para inspeccionar la imagen/config, pero no cambies políticas de producción sin entender el efecto.

## Healthcheck

Un contenedor `running` puede tener app rota. El healthcheck debe probar una condición representativa y ser barato. Evita depender de herramientas que la imagen final no contiene.

## Debug de imagen mínima

No instales shells/debuggers dentro de la imagen de producción sólo para inspeccionarla. Usa un stage/debug image aparte, `docker cp`, inspect/logs, o contenedores auxiliares cuando sea apropiado.

## Señales

Si stop tarda hasta timeout y luego mata el proceso, revisa PID 1/ENTRYPOINT y que el proceso reciba SIGTERM. Scripts wrapper deben usar `exec` cuando corresponda.

## Cambios de configuración

`docker restart` reutiliza el contenedor existente. Cambios en Compose/env/mounts normalmente requieren recreación (`docker compose up -d ...`). Cambios en Dockerfile requieren build nuevo.
