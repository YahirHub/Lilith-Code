# Desarrollo y validación de Lilith

## Restricciones del proyecto

- Go según `go.mod`, UI terminal nativa con tview/tcell y componentes internos `uikit`.
- Builds oficiales con `CGO_ENABLED=0` y tag `grammar_set_core`.
- Mantén Linux, Windows y Android/Termux como targets reales; no asumas Bash o filesystem POSIX en código portable.
- Los slash commands generales pertenecen a `modules/core/<feature>`; la TUI sólo adapta el host estable.

## Validación normal

```sh
gofmt -w <archivos-go-editados>
git diff --check
go test -mod=readonly -tags=grammar_set_core ./...
go test -race -mod=readonly -tags=grammar_set_core ./...
go vet -mod=readonly -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core ./cmd/li
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -tags=grammar_set_core -o /tmp/lilith.exe ./cmd/li
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -mod=readonly -tags=grammar_set_core ./...
```

Adapta el path temporal a la plataforma. No conserves binarios de validación dentro del repo.

## Cambios importantes

- Lee `AGENTS.md`, `LILITH.md` si existe y los documentos numerados de `contexto/` antes de modificar arquitectura.
- Añade un documento `contexto/NNN-*.md` para decisiones relevantes y actualiza `contexto/000-contexto-maestro.md` cuando cambien invariantes.
- Preserva cambios ajenos del working tree; revisa diff, pruebas y estado antes del commit.
- Commits en español, con asunto concreto y descripción útil. No reescribas historia existente para ocultar avances.
