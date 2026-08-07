---
name: docker-development
description: Use for Docker and Docker Compose development, Dockerfile design, builds, containers, logs/exec/inspect, networks, volumes/bind mounts, registries, image publishing, debugging, security, cleanup and production-oriented container workflows.
user-invocable: true
model: inherit
argument-hint: "[tarea Docker/Compose concreta]"
when_to_use: |
  Any task involving Dockerfile, docker build/run/exec/logs/inspect, Compose services, container startup, ENTRYPOINT/CMD, images, networks, ports, volumes, bind mounts, registries, healthchecks, permissions, multi-stage/static builds or Docker cleanup.
---

# Docker — índice modular

**No cargues toda la skill.** Este `SKILL.md` sólo enruta. Usa `skill_read` para abrir los módulos estrictamente necesarios.

## Diagnóstico inicial

Antes de cambiar Docker:

1. Identifica `Dockerfile*`, `compose*.yml/yaml`, `.dockerignore`, entrypoints y variables de entorno relevantes.
2. Determina si el problema ocurre en build, create/start, runtime, red, almacenamiento, permisos o healthcheck.
3. Comprueba `docker version` y `docker compose version` si vas a ejecutar Docker. Docker es externo a Lilith.
4. Preserva datos persistentes: nunca conviertas un problema de contenedor en pérdida de volumen.

## Enrutador

| Necesidad | Recurso |
|---|---|
| run/start/stop/restart/exec/logs/inspect/cp | `references/containers.md` |
| Dockerfile, BuildKit, multi-stage, cache, ENTRYPOINT/CMD | `references/dockerfile-build.md` |
| Compose: up/down/build/pull/exec/logs/profiles/depends_on | `references/compose.md` |
| puertos, DNS, redes, localhost, host/container | `references/networking.md` |
| volumes, bind mounts, permisos, backups, datos | `references/storage.md` |
| healthchecks, crash loops, debug de arranque y procesos | `references/debugging.md` |
| usuario no-root, secrets, capabilities, superficie de imagen | `references/security.md` |
| tags, registry, push/pull, plataformas, release de imagen | `references/registry-release.md` |
| rm/prune/limpieza y recuperación sin pérdida de datos | `references/cleanup.md` |

## Principios

- Usa Compose v2 (`docker compose`), no asumas el binario legado `docker-compose`.
- Prefiere imágenes finales pequeñas y multi-stage cuando separan build/runtime de forma real.
- No copies secretos a capas de imagen ni los pongas en `ARG`/`ENV` de build si deben permanecer secretos.
- Un volumen es dato; un contenedor es proceso descartable. No elimines volúmenes para arreglar un contenedor salvo instrucción explícita y backup/aceptación de pérdida.
- Antes de editar, inspecciona logs y estado real. Después, rebuild/recreate sólo los servicios afectados y vuelve a revisar logs/health.
- No expongas puertos al host si sólo necesitan comunicación interna entre servicios.

## Cierre

Después del cambio, valida como corresponda:

```sh
docker compose config
docker compose build <servicio>
docker compose up -d <servicio>
docker compose ps
docker compose logs --tail=200 <servicio>
```

Adapta los comandos al proyecto; no levantes servicios de producción contra datos reales sólo para probar una edición local.
