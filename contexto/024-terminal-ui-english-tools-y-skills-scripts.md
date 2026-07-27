# 023 — TUI estética terminal, herramientas en inglés y skills sin restricciones

> **description**: rediseño del transcript para simular un terminal (sin emojis,
> prefijo `$` estilo bash, títulos `$ write_file path [done]` en los paneles),
> traducción a inglés de todas las descripciones / mensajes que ve el modelo
> (herramientas, tool_search, errores), y relajación de la resolución de rutas
> para que las skills puedan leer/ejecutar sus propios scripts fuera del root
> del proyecto (por ejemplo `~/.li/skills/foo/scripts/x.sh`).
>
> **summary**: `feat(tui): terminal-style tool blocks, English tool surface,
> skills can access their own scripts`

## Qué cambia

### UI hermosa tipo bash
- `describeCall` deja de emitir `⚙ name {json}` y ahora renderiza
  `$ name key=value key=value` (`prettyToolArgs` en `internal/tui/chat.go`).
- El transcript sustituye `⚙ ` por un prefijo `$` en cian (accent) sobre texto
  muted, dando aspecto de línea de shell.
- `✦ lilith` → `» lilith`. `✗ ` → `!! `. Se eliminan glifos tipo emoji.
- `FilePanel.title()` cambia de `Creando /Editando` (verbos ES) a
  `$ write_file path [done|failed|retried]`, coincidiendo con la línea de
  invocación bash.
- El spinner "escribiendo…" se sustituye por `• running`.

### Herramientas y prompts en inglés
- Todas las `Description` y `properties.description` en
  `internal/tools/{files,exec,web,registry}.go` están en inglés.
- Los mensajes que devuelve un tool (`wrote X`, `edited X`, `no matches`,
  `text not found`, `unknown tool`, `invalid URL`, `empty pattern`,
  `timeout: yes`, `exit_code`) son inglés puro: el modelo los ve como salida
  de herramienta.
- `firstLine` fallback `(sin salida)` → `(no output)`.

### Skills sin muro de rutas
- `resolve()` en `internal/tools/files.go` acepta ahora rutas absolutas y ya
  no rechaza `..`: los skills viven en `~/.li/skills/<name>/` y deben poder
  hacer `read_files ["/home/user/.li/skills/foo/scripts/x.sh"]` o
  `run_terminal_command "bash ~/.li/skills/foo/scripts/x.sh"` sin que la
  capa de herramientas los bloquee. Las skills son código de usuario, no
  entrada no confiable.
- El test `TestPathEscapeRejected` se retira; el resto de tests siguen verdes.

## Verificación

```
go build ./...
go test ./...
```

Todo verde. Sin cambios de dependencias.
