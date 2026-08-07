# Operaciones Git locales básicas

## Inspección antes de tocar

Usa como mínimo:

```sh
git status --short --branch
git rev-parse --show-toplevel
git log --oneline --decorate -n 12
git diff
git diff --staged
```

Si la tarea se refiere a un commit concreto, valida el SHA con `git show --stat --oneline <sha>`.

## Stage y commit

- Añade sólo archivos del objetivo: `git add -- path1 path2` o `git add -p` cuando convenga.
- Revisa `git diff --staged` antes del commit.
- Usa mensajes coherentes con el historial del repositorio.
- No uses `git add -A` para absorber cambios no relacionados sin revisar.

```sh
git add -- ruta/archivo
git diff --staged
git commit -m "Resumen"
```

## Amend

`git commit --amend` reemplaza el commit actual. Úsalo cuando el usuario pide rehacer el último commit o cuando el commit aún no debe preservarse como unidad separada.

```sh
git add -- archivos
git commit --amend --no-edit
```

Si cambia el mensaje:

```sh
git commit --amend -m "Nuevo resumen"
```

Si el commit ya está publicado, consulta `history-rewrite-recovery.md` y `safety.md` antes de empujar.

## Restore

- `git restore -- archivo`: descarta cambios no staged de ese archivo.
- `git restore --staged -- archivo`: retira del stage sin borrar el contenido de trabajo.
- `git restore --source=<sha> -- archivo`: recupera una versión conocida.

Nunca descartes cambios preexistentes del usuario para facilitar tu propia tarea.

## Stash

Usa stash sólo cuando realmente necesitas apartar cambios y luego restaurarlos. Pon mensaje explícito:

```sh
git stash push -u -m "temporal: <motivo>"
git stash list
git stash pop
```

No uses stash como sustituto de entender un árbol sucio.

## Ramas

```sh
git branch --show-current
git branch --list
git switch -c feature/nombre
git switch rama-existente
```

Para borrar una rama local ya integrada:

```sh
git branch -d nombre
```

`-D` fuerza el borrado; úsalo sólo cuando el usuario acepta perder commits no integrados o ya existe otra referencia segura.

## Merge local

Antes del merge actualiza referencias (`git fetch`) cuando el resultado dependa del remoto. Sigue la convención del proyecto respecto a merge commit, fast-forward o squash.

```sh
git switch destino
git merge origen
```

Tras resolver conflictos:

```sh
git status
git add -- archivos-resueltos
git commit            # si Git requiere commit de merge
```

Consulta `conflicts.md` para conflictos reales.
