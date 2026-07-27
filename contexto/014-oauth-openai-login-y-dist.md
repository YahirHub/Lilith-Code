---
Summary: Login OAuth ChatGPT/Codex + build en dist/ y README limpio
Description: |
  - Añade el flujo OAuth compatible con Codex (PKCE + callback local en
    http://localhost:1455/auth/callback y device code como fallback) en
    internal/providers/openai/chatgpt_oauth.go.
  - Registra el proveedor bundled `openai-codex` con el catálogo de modelos
    Codex; los tokens se guardan en provider-auth.json bajo la clave OAuth
    existente. Tras el login, el proveedor queda activo automáticamente.
  - Rehace internal/tui/login_codex.go como asistente real (arranque, espera
    de callback, cambio a device code con D, éxito y errores).
  - `make build` ahora genera todo dentro de dist/: binario `li` + toolchain
    externa (ripgrep, busybox.exe) en dist/tools/bin. cmd/build acepta
    `-dir <ruta>` y toolchain.BinDir respeta la variable LI_TOOLS_DIR.
  - `make build-cross` añade li.exe para Windows.
  - .gitignore excluye original/, dist/, bin/ y *.exe.
  - README.md pasa a ser la portada del agente (sin referencias ficticias) y
    los pasos de instalación viven en install.md.
---

# 014 – Login OAuth ChatGPT/Codex y empaquetado en dist/

## Qué cambia

- `internal/providers/openai/chatgpt_oauth.go` con constantes oficiales
  (`app_EMoamEEZ73f0CkXaXp7hrann`, endpoints `auth.openai.com`) y helpers
  para PKCE, callback y device code. Tokens persistidos vía
  `internal/secrets` bajo `openai-codex`.
- `internal/providers/bundled.go` expone Codex como proveedor bundled con
  `AuthOAuth`; requiere `/login` para tener tokens válidos.
- `internal/tui/login_codex.go` implementa el asistente OAuth completo.
- `Makefile` y `cmd/build/main.go` empaquetan binario + toolchain en
  `dist/`. `LI_TOOLS_DIR` sigue permitiendo el uso portable en runtime.
- `.gitignore`, `README.md` e `install.md` actualizados.

## Comandos de verificación

```bash
go vet ./...
go build ./...
go test ./...
make build
```
