# Historia: eliminar commits, rebase, revert y recuperación

## Decide primero: ¿publicado o no publicado?

Inspecciona rama/upstream:

```sh
git status --short --branch
git branch -vv
git log --oneline --decorate --graph -n 20
```

### Historia no publicada

Para eliminar los últimos N commits conservando cambios staged:

```sh
git reset --soft HEAD~N
```

Conservando cambios en working tree pero no staged:

```sh
git reset --mixed HEAD~N
```

Para descartar commits **y sus cambios**:

```sh
git reset --hard HEAD~N
```

`--hard` es destructivo. No lo uses si hay cambios del usuario que no estén protegidos.

### Historia ya publicada/compartida

Preferir revert:

```sh
git revert <sha>
```

Para varios commits, revisa orden/rango y el diff generado. Un revert crea historia nueva y es la opción colaborativa segura.

## Eliminar o editar un commit que no es HEAD

Para commits locales/no compartidos usa rebase interactivo:

```sh
git rebase -i <base>
```

Acciones comunes: `drop`, `reword`, `edit`, `squash`, `fixup`. Si no puedes usar editor interactivo de forma segura, construye una estrategia no interactiva explícita o pide la operación exacta; no improvises sed sobre el todo-list sin inspeccionarlo.

## Cherry-pick

```sh
git cherry-pick <sha>
```

Para un rango, confirma primero el orden con `git log`. Si hay conflictos, usa `git cherry-pick --continue` después de resolverlos o `git cherry-pick --abort` para volver al estado previo.

## Recuperación con reflog

Antes/después de una reescritura importante:

```sh
git rev-parse HEAD
git reflog --date=local -n 30
```

Para proteger el punto previo puedes crear una rama temporal:

```sh
git branch backup/antes-de-rewrite <sha>
```

Si un reset/rebase eliminó una referencia visible, encuentra el SHA en reflog y restaura una rama o vuelve a ese commit sólo después de confirmar que es el punto correcto.

## Rebase sobre upstream

```sh
git fetch origin
git rebase origin/main
```

No rebases automáticamente ramas compartidas. Si la rama ya está publicada y necesitas actualizarla tras rebase, consulta `remotes-sync.md` y usa `--force-with-lease` sólo bajo la política de `safety.md`.
