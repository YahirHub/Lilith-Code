# Seguridad Docker

## Usuario

Cuando la aplicación no requiere root, ejecuta como usuario no privilegiado. Si el entrypoint necesita root para crear/chown de volúmenes, limita esa fase y baja privilegios antes de iniciar la app cuando sea viable.

## Secrets

No:
- copies `.env` secretos a la imagen;
- pongas tokens en Dockerfile `ENV`/`ARG` pensando que desaparecen;
- imprimas secretos en logs/build output;
- montes Docker socket sin necesidad.

Usa mecanismos de secrets/credenciales del entorno y mínimos permisos.

## Capabilities / privileged

Evita `--privileged`. Añade sólo capabilities/dispositivos necesarios. Montar `/var/run/docker.sock` da control muy amplio sobre el daemon y debe tratarse como privilegio de host.

## Filesystem y mounts

Usa mounts de solo lectura cuando aplique. Separa datos mutables de código/config. No hagas bind de `/` o directorios sensibles del host como atajo.

## Puertos

No publiques servicios internos por comodidad. Limita bind de host y firewall según necesidad.

## Supply chain

- Bases mantenidas y actualizadas.
- Dependencias mínimas.
- Multi-stage para retirar toolchains del runtime.
- CI que reconstruya/pruebe imágenes.
- Considera SBOM/provenance/signing cuando el flujo de release lo requiera.
