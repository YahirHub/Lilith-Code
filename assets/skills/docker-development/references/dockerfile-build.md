# Dockerfile y builds

## Diagnóstico

Lee Dockerfile, `.dockerignore`, contexto de build y scripts de entrypoint. Distingue build-time de runtime.

```sh
docker build --check -t app:test .
docker history app:test
```

## Multi-stage

Separa toolchain de build y runtime cuando reduce superficie/tamaño. Copia al stage final sólo artefactos necesarios.

Para binarios Go estáticos es común:

```dockerfile
FROM golang:<version> AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/app

FROM scratch
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
```

Sólo usa `scratch` si la app realmente no necesita certificados, timezone, shell, archivos de usuario u otras piezas runtime.

## Cache

Ordena pasos para que dependencias estables se cacheen antes del código cambiante. Mantén `.dockerignore` para excluir `.git`, artefactos, secretos y directorios pesados.

Para un build limpio de diagnóstico:

```sh
docker build --pull --no-cache -t app:test .
```

No uses `--no-cache` en cada build normal sin motivo.

## COPY vs ADD

Prefiere `COPY` para archivos locales simples. Usa `ADD` sólo cuando necesitas su semántica específica.

## ENTRYPOINT y CMD

Usa forma exec JSON para señales/argumentos predecibles:

```dockerfile
ENTRYPOINT ["/app"]
CMD ["serve"]
```

Si usas script de entrypoint, termina con `exec "$@"` para que el proceso de aplicación reciba señales como PID 1.

## Imagen base

Usa bases pequeñas pero operables y mantenidas. Fija al menos versión/tag apropiado; para reproducibilidad estricta puedes fijar digest. Actualiza conscientemente las bases para recibir parches.

## Build secrets

No copies tokens/SSH keys dentro del contexto o capas. Usa capacidades seguras de BuildKit cuando el build requiera secretos/SSH.

Referencia oficial para detalles actuales: https://docs.docker.com/build/building/best-practices/
