# 135. Arquitectura de módulos estáticos y distribuciones privadas

## Objetivo

Permitir que Lilith mantenga un repo público de propósito general y que una
empresa mantenga un repo/branch privado con integraciones propias que pueda
absorber periódicamente `main` sin editar el dispatcher central de comandos.

El sistema se enfoca inicialmente en comandos slash y rutas slash dinámicas. No
usa plugins binarios: todos los módulos siguen compilados dentro del mismo
binario Go estático.

## Arquitectura

### `internal/moduleapi`

Se introduce una API estable versión 1 con:

- `Definition`: identidad, versión, source y dependencias del módulo;
- `Command`: slash command exacto y aliases;
- `Route`: prefijo dinámico como `/skill:<nombre>`;
- `Host`: frontera estable entre un módulo y Lilith;
- `Registry`: validación, resolución, diagnóstico y dispatch metadata;
- catálogo estático de módulos registrados mediante `moduleapi.Register`.

Un módulo no recibe `*tui.ChatModel`. El adaptador `internal/tui/module_host.go`
es la única capa que traduce operaciones estables del host a la TUI. `Host` se
mantiene pequeño y las operaciones opcionales (`SkillInvoker`, `Submitter`,
`ScreenOpener`, `RewindState`) viven en interfaces de capacidad independientes;
así añadir una capacidad nueva al core no rompe por interfaz todos los módulos
de una distribución privada después de un merge.

### Validación fail-closed

El registry no permite sobrescrituras silenciosas:

- API incompatible -> módulo disabled;
- ID duplicado -> módulo disabled;
- dependencia `Requires` ausente/disabled -> módulo disabled;
- command/alias/ruta en conflicto -> módulo posterior disabled;
- un command exacto dentro del namespace de una route (o una route que abarque un command existente) también se considera conflicto;
- las rutas dinámicas deben terminar en `:` para no interceptar comandos exactos;
- el namespace `core.*` está reservado a source `builtin`/`core`;
- módulos `core.*` siempre tienen prioridad sobre extensiones downstream.

`/modules` muestra los módulos enlazados, source, versión, comandos/rutas,
dependencias y motivo de deshabilitación.

### Módulos built-in iniciales

- `core.commands`: adapta todos los slash commands históricos al registry sin
  cambiar su implementación/UX;
- `core.rewind`: mueve `/rewind` fuera de la lista central;
- `core.skills`: mueve `/skills:*` y `/skill:*` fuera del `submit` central;
- `core.modules`: aporta `/modules [id]` para diagnóstico.

El dispatcher de `ChatModel` ya no conoce de forma especial los prefijos
`skill:`/`skills:`; resuelve rutas dinámicas desde el registry. La búsqueda de
la paleta también deriva el target desde `FindModuleRoute`, por lo que no conserva
un parser paralelo de esos prefijos en el core.

### Selección de distribución

`internal/distribution/builtin.go` blank-importa únicamente módulos públicos.
Una downstream privada agrega, sin editar ese archivo:

```go
//go:build company

package distribution

import _ "github.com/lilith/li/modules/company/jsecure"
```

`cmd/build` incorpora:

```text
go run ./cmd/build build --distribution company
```

que mantiene `CGO_ENABLED=0` y compila con:

```text
-tags=grammar_set_core,company
```

La distribución pública (`go run ./cmd/build build`) conserva solamente
`grammar_set_core` y no cambia su comportamiento.

## Estrategia Git público/privado

Repo público:

```text
main
  core
  internal/moduleapi
  internal/distribution/builtin.go
```

Repo privado:

```text
company
  + modules/company/**
  + internal/distribution/company.go
  + assets privados opcionales en paths nuevos
```

El privado conserva `main` como espejo del público y `company` como branch de
distribución. Configura el público como `upstream` y hace:

```bash
git fetch upstream
git switch main
git merge --ff-only upstream/main
git switch company
git merge main
```

Las integraciones empresariales viven en archivos que el público no modifica,
por lo que los merges normales no necesitan conflictos artificiales en
`chat.go`, `commands.go` o providers.

## Builder

`cmd/build` acepta `--distribution <tag>` sólo para la acción `build`. `default`,
`main` y `public` conservan la build pública. Los demás nombres válidos agregan
el tag indicado además de `grammar_set_core`.

Se añaden tests de parsing y tags para impedir que una distribución privada
elimine accidentalmente la gramática embebida.

## Pruebas añadidas

- registry resuelve comandos, aliases y rutas;
- missing dependencies deshabilitan módulos;
- colisiones con core fallan de forma segura;
- API incompatible se rechaza;
- prioridad `core.*` sobre downstream independientemente del orden de init;
- todos los slash commands de TUI tienen `ModuleID`;
- `/rewind` pertenece a `core.rewind`;
- `/skill:*` pertenece a `core.skills`;
- `/modules` muestra los módulos esperados;
- builder agrega `company` sin perder `grammar_set_core`.

## Validación realizada en el entorno de entrega

- `gofmt` aplicado a todos los Go modificados;
- `git diff --check` limpio;
- parser de Go sobre 302 archivos: 0 errores sintácticos;
- `internal/moduleapi` y los tres built-ins probados en un módulo aislado con
  Go local: PASS; `internal/moduleapi` también pasa `go test -race`;
- simulación real del build tag `company`: sin tag no aparece `company.demo`;
  con `-tags=company` aparece junto a los módulos core;
- `cmd/build` + `internal/toolchain` + `internal/config` probados en módulo
  aislado: PASS, incluidos los nuevos tests de distribución; `cmd/build` también
  pasa `go test -race` en ese harness;
- el registry rechaza tanto comandos exactos dentro de namespaces dinámicos como
  rutas que intenten abarcar comandos ya registrados, y reserva `core.*` al core.

La suite integral del repositorio requiere el toolchain declarado por Lilith
(`go 1.25.12`). El entorno de entrega dispone sólo de Go 1.23.2, por lo que la
suite completa debe volver a ejecutarse con `test.cmd` en Windows/GitHub Actions.

## Archivos principales

- `internal/moduleapi/**`
- `internal/modules/builtin/**`
- `internal/distribution/builtin.go`
- `internal/tui/module_host.go`
- `internal/tui/commands.go`
- `internal/tui/chat.go`
- `cmd/build/main.go`
- `docs/modules/**`
- `modules/README.md`
