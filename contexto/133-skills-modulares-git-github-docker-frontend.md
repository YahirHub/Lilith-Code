# 133. Skills modulares Git/GitHub, Docker y auditoría frontend aislada

# Fecha

2026-08-07

# Objetivo

Elevar la versión de Lilith a `0.3.2` porque el release/tag `v0.3.1` ya existe y ampliar los assets embebidos con conocimiento operativo reusable para Git/GitHub, Docker y desarrollo frontend sin inflar el contexto principal.

# Decisiones tomadas

- `internal/version/version.go` se mantiene como fuente única de versión y pasa a `0.3.2`.
- Las skills grandes nuevas usan `SKILL.md` únicamente como índice/enrutador y colocan los procedimientos detallados en `references/*.md`.
- El runtime existente ya materializa y embebe todo el árbol de una skill, por lo que `skill_files`, `skill_search` y `skill_read` permiten divulgación progresiva sin código de loader adicional.
- Git/GitHub y Docker permanecen como ejecutables externos (`git`, `gh`, `docker`). Las skills enseñan a descubrirlos y usarlos; no se embeben binarios ni se añade CGO.
- La auditoría frontend exhaustiva se delega a un subagente aislado `frontend-browser-auditor`. Es foreground por defecto y recibe sólo la tarea delegada, su prompt y la skill frontend; el padre recibe un handoff compacto en vez de snapshots/logs completos.
- El auditor no tiene herramientas de edición/escritura. Tiene lectura, búsqueda, terminal acotada por instrucciones, navegación y acceso a módulos de skill.

# Arquitectura actual

## `git-github`

`assets/skills/git-github/SKILL.md` enruta a módulos para:

- operaciones locales;
- reescritura/eliminación de commits y reflog;
- sincronización/push/remotos/tags;
- GitHub CLI (`gh`), PRs, Actions y releases;
- búsqueda/eliminación de contenido del repositorio;
- conflictos;
- política de seguridad para historia/remotos.

La regla principal es preservar cambios ajenos, preferir `revert` para historia compartida y usar `--force-with-lease` sólo cuando una reescritura remota fue solicitada claramente.

## `docker-development`

`assets/skills/docker-development/SKILL.md` enruta a módulos para:

- contenedores;
- Dockerfile/build;
- Compose;
- networking;
- storage;
- debugging/health;
- seguridad;
- registry/release;
- cleanup seguro.

Se preserva `docker compose` v2 como sintaxis principal y se trata un volumen como dato persistente, no como objeto descartable.

## `frontend-development`

`assets/skills/frontend-development/SKILL.md` establece una regla obligatoria: inspeccionar la UI real antes/después de cambios cuando exista una app ejecutable y revisar consola/red del navegador. Para 3+ páginas o una auditoría amplia se delega al agente `frontend-browser-auditor`.

Los módulos cubren workflow, browser audit, console/network, forms/states, responsive/accessibility y reporte de verificación.

## `frontend-browser-auditor`

`assets/agents/frontend-browser-auditor.md`:

- inventaría rutas reales desde código/navegación;
- usa una sesión de navegador dedicada;
- revisa DOM, console y network por página;
- prueba interacciones no destructivas;
- no modifica archivos ni Git;
- usa `fill_secret` para secretos;
- devuelve al padre sólo cobertura, fallos, severidad, reproducción mínima y evidencia relevante.

El agente precarga `frontend-development` y puede leer sólo los references necesarios mediante las herramientas de skill.

# Librerías usadas

No se añaden dependencias. Se reutilizan:

- runtime de skills existente (`internal/skills`);
- subagentes existentes (`internal/agents`, `internal/subagents`);
- controlador de navegador Chromedp existente (`internal/browser`, tool `browser`);
- herramientas de terminal/búsqueda existentes.

# Fuentes oficiales revisadas

Para mantener los procedimientos alineados con CLIs actuales se revisaron:

- GitHub CLI manual (`cli.github.com/manual`), incluidos `gh repo`, `gh pr`, `gh workflow` y `gh release`;
- documentación oficial Git (`git-scm.com/docs`) para historia/remotos/recuperación;
- Docker Docs (`docs.docker.com`) para build, Compose, networking, storage y bind mounts.

Las skills indican volver a documentación oficial cuando una operación dependa de flags/versiones actuales.

# Archivos importantes modificados

- `internal/version/version.go`
- `install.md`
- `assets/skills/README.md`
- `assets/skills/git-github/**`
- `assets/skills/docker-development/**`
- `assets/skills/frontend-development/**`
- `assets/agents/README.md`
- `assets/agents/frontend-browser-auditor.md`
- `internal/tools/browser.go`
- `internal/tools/exec.go`
- `internal/tools/registry.go`
- `internal/tui/chat.go`
- pruebas de assets, agentes, subagentes y selección de tools.

# Problemas encontrados

- `v0.3.1` ya existe, por lo que el workflow de release aborta correctamente para no sobrescribir una publicación existente.
- Un auditor de navegador en modo background no puede usar actualmente `browser` porque la whitelist de background no lo expone. No se amplía esa superficie: un subagente foreground ya mantiene el contexto detallado aislado y evita prompts secretos/ventanas en background.

# Soluciones implementadas

- Versión elevada a `0.3.2` y ejemplos de instalación sincronizados.
- Skills Git/GitHub, Docker y frontend embebidas y modulares.
- Agente frontend de navegador aislado y sin herramientas de edición.
- Prompt principal y guidelines de browser recomiendan delegación para auditorías amplias.
- Tool selection reconoce explícitamente consultas GitHub/`gh`, Docker/Compose y auditoría de todas las páginas.
- Pruebas verifican materialización de índices/references, metadata del auditor, política de tools y selección lazy.

# Validación realizada

- `gofmt` sobre todos los archivos Go modificados.
- `git diff --check` sin errores.
- Validación de índices: cada ruta `references/*.md` declarada en los tres `SKILL.md` existe.
- Pruebas `internal/skills` e `internal/agents` ejecutadas correctamente en una copia temporal con el Go local, validando materialización/parseo de los nuevos assets sin modificar el `go.mod` real.
- Las pruebas de `internal/tools`/`internal/subagents` no pudieron ejecutarse en este entorno porque sus dependencias externas no están cacheadas y la red del contenedor no puede acceder a `proxy.golang.org`.
- El repositorio real conserva `go 1.25.12`; no se alteró el toolchain ni `go.sum`.

# Pendientes

- Ejecutar la suite integral con la versión Go declarada por el repositorio antes del release.
- Hacer una auditoría real de una app multipágina para validar el handoff compacto del nuevo subagente.

# Próximos pasos

1. Ejecutar `go mod tidy -diff` y `test.cmd` en Windows con el toolchain del proyecto.
2. Probar `/skills:git-github`, `/skills:docker-development` y `/skills:frontend-development`.
3. Pedir al agente principal una auditoría frontend amplia y verificar que delegue a `frontend-browser-auditor`.
4. Publicar `v0.3.2` sólo cuando las pruebas estén verdes.
