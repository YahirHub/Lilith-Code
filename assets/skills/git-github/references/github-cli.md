# GitHub CLI (`gh`)

## Disponibilidad y autenticación

```sh
gh --version
gh auth status
```

No solicites ni imprimas tokens si ya existe una sesión. `gh auth login` es interactivo; úsalo sólo si el usuario necesita autenticar el entorno.

## Repositorios

```sh
gh repo view --json nameWithOwner,defaultBranchRef,url,visibility
gh repo read-dir [DIRECTORY] -R OWNER/REPO
gh repo read-file PATH -R OWNER/REPO
gh repo clone OWNER/REPO
gh repo fork OWNER/REPO --clone=false
gh repo sync OWNER/REPO
```

`gh repo read-dir` y `gh repo read-file` permiten inspeccionar un repositorio remoto sin clonar cuando sólo necesitas inventario o contenido puntual. Para cambios, prefiere un checkout local verificable.

Para crear/renombrar/archivar/eliminar repositorios, valida primero el repo objetivo. `gh repo delete` es destructivo y requiere petición inequívoca.

## Pull requests

Inspección:

```sh
gh pr status
gh pr list
gh pr view <n> --json number,title,state,headRefName,baseRefName,mergeable,statusCheckRollup
gh pr diff <n>
gh pr checks <n>
```

Crear:

```sh
gh pr create --fill
```

Merge sólo cuando el usuario pidió integrar y los checks/políticas lo permiten:

```sh
gh pr merge <n> --merge
# o --squash / --rebase según la política del repo
```

`--auto` puede dejar el merge pendiente hasta que pasen requisitos. `--admin` omite protecciones: no usarlo salvo solicitud explícita y justificada.

Para revertir un PR ya fusionado, `gh pr revert <n>` crea una reversión mediante PR cuando está disponible; revisa el resultado antes de publicarlo.

## Issues y búsqueda

```sh
gh issue list
gh issue view <n>
gh search issues "texto" --repo OWNER/REPO
gh search prs "texto" --repo OWNER/REPO
gh search code "simbolo" --repo OWNER/REPO
```

Para código que ya está clonado, preferir `code_search` porque da resultados locales deterministas y no consume API.

## Actions

```sh
gh workflow list
gh run list --limit 20
gh run view <id> --log-failed
gh workflow run workflow.yml --ref rama
```

`gh workflow run` sólo funciona para workflows que aceptan `workflow_dispatch`.

No canceles/reintentes workflows ni borres caches/runs salvo que sea parte de la solicitud.

## Releases

```sh
gh release list
gh release view vX.Y.Z
gh release create vX.Y.Z --generate-notes
gh release upload vX.Y.Z archivo
```

Antes de crear release verifica que tag/release no existan. Si una versión ya existe, eleva la versión en la fuente de verdad del proyecto en vez de sobrescribir una publicación existente, salvo que el proceso del repo diga lo contrario.

## API

Usa `gh api` cuando no exista un comando de alto nivel adecuado. Antes de `POST/PATCH/PUT/DELETE`, identifica endpoint, repo, branch y payload. Para operaciones destructivas, muestra claramente qué recurso se mutará.

Fuentes oficiales de referencia cuando necesites validar flags actuales:
- https://cli.github.com/manual/
- https://docs.github.com/
