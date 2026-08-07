# Registry y release de imágenes

## Tagging

Evita depender únicamente de `latest` para releases reproducibles. Publica tags versionados y, si el proceso lo requiere, un alias estable adicional.

```sh
docker tag app:local REGISTRY/OWNER/app:1.2.3
docker push REGISTRY/OWNER/app:1.2.3
```

## Login

Usa el credential store/login seguro disponible. No pases contraseñas/tokens visibles en comandos o documentación.

## Pull

```sh
docker pull IMAGE:TAG
docker image inspect IMAGE:TAG
```

Verifica digest cuando necesitas demostrar qué artefacto se desplegó.

## Multi-platform

Si publicas amd64/arm64, usa Buildx/BuildKit y prueba que el Dockerfile no introduzca binarios de arquitectura equivocada.

```sh
docker buildx build --platform linux/amd64,linux/arm64 --push -t IMAGE:TAG .
```

No anuncies una plataforma que no fue construida/probada.

## Compose build --push

Compose puede construir y empujar imágenes declaradas; valida nombres/tags interpolados antes de usar `--push`.

## Release

Antes de publicar:
- build limpio razonable;
- tests;
- revisar tamaño/capas;
- confirmar versión/tag no existente o política de overwrite;
- publicar;
- verificar digest remoto/despliegue.
