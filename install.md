# Instalación de Lilith

## Requisitos

- Go 1.24 o superior (`go version`).
- `git` y `make` disponibles en el PATH.
- Windows, macOS o Linux (amd64 o arm64).

## Compilar desde el código

```bash
git clone <URL-de-tu-repo> lilith
cd lilith
make build
```

`make build` genera:

- `dist/li` (o `dist/li.exe` en Windows) — el binario principal.
- `dist/tools/bin/` — utilidades externas (ripgrep, `busybox.exe` en Windows)
  descargadas y verificadas por SHA-256.

Para compilar además el binario Windows desde otro sistema:

```bash
make build-cross
```

## Ejecutar

```bash
./dist/li
```

En Windows, `dist\li.exe`.

Al primer arranque, Lilith crea `~/.li/` con:

- `settings.json` — preferencias.
- `providers.json` — proveedores configurados.
- `provider-auth.json` — API keys y tokens OAuth (permisos `0600`).
- `sessions/` — historial de conversaciones.

## Configurar el proveedor de IA

Desde la TUI:

- **OpenCode Free**: seleccionable en el onboarding, sin login.
- **ChatGPT Codex**: `/login` → *ChatGPT Codex*. Usa una suscripción
  ChatGPT Plus/Pro y abre el navegador para el flujo OAuth con PKCE. Si estás en un entorno headless,
  pulsa `D` para usar código de dispositivo.
- **Proveedor personalizado**: `/login` → *Proveedor personalizado* e
  introduce nombre, base URL (OpenAI-compatible) y API key.

## Instalación global (opcional)

Copia el binario a un directorio del `PATH`:

```bash
sudo install -m 0755 dist/li /usr/local/bin/li
```

o en Windows, mueve `dist\li.exe` a una carpeta del `PATH`.

Para reinstalar la toolchain externa:

```bash
make tools           # descarga sólo lo que falte
go run ./cmd/build install -f   # fuerza reinstalación
```
