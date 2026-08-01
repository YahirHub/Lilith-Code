# 090 — Compatibilidad de tests MCP y Rewind en Windows

## Fallos observados

Al ejecutar `go test ./... -count=1` en Windows aparecieron tres falsos o reales negativos específicos de plataforma:

1. `internal/mcp` comparaba literalmente una ruta expandida que contenía separadores mixtos (`tmp\plugin/bin/server`) con una ruta nativa (`tmp\plugin\bin\server`). Ambas representan el mismo path para Windows.
2. `internal/rewind` restauraba contenido LF como CRLF cuando la configuración global de Git tenía `core.autocrlf=true`.
3. El mismo cambio de fin de línea podía afectar el workspace creado por `/fork`.

## Correcciones

### MCP

La prueba compara ahora `filepath.Clean` de ambos valores. No se modifica la configuración MCP ni se reescriben argumentos arbitrarios: la prueba verifica equivalencia semántica de rutas en vez de exigir un separador concreto del sistema operativo.

### Rewind y fork

Las operaciones Git que capturan o materializan archivos incorporan configuración local por invocación:

```text
-c core.autocrlf=false
-c core.safecrlf=false
```

Se aplica a:

- `git add -A` sobre el índice temporal del checkpoint;
- `git checkout-index` durante la restauración;
- `git worktree add` al crear el fork.

Esto evita que la preferencia global del usuario transforme los bytes del snapshot. No se modifica `.git/config`, la configuración global, el staging real ni el comportamiento Git normal del proyecto. Los atributos definidos por el propio repositorio continúan siendo autoritativos.

La prueba principal de Rewind configura deliberadamente `core.autocrlf=true`, de modo que esta regresión se cubre también al ejecutar la suite en Linux.

## Validación esperada en Windows

```powershell
go test ./internal/mcp ./internal/rewind -count=1
go test ./... -count=1
```

Los paquetes MCP y Rewind deben terminar con `ok`.
