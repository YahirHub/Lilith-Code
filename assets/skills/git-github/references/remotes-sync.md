# Remotos, sincronización y push

## Inspección

```sh
git remote -v
git branch -vv
git status --short --branch
git fetch --all --prune
```

No cambies `origin` sin verificar qué repositorio representa.

## Upstream de rama

Primer push:

```sh
git push -u origin nombre-rama
```

Después:

```sh
git push
```

## Pull

Evita `git pull` ciego cuando puede crear un merge inesperado. Inspecciona divergencia y elige conforme al proyecto:

```sh
git fetch origin
git log --oneline --left-right HEAD...origin/main
```

Luego una de:

```sh
git merge origin/main
# o
git rebase origin/main
```

## Publicar un commit corregido

Si sólo hiciste amend/rebase de una rama privada ya publicada y el usuario pidió actualizarla:

```sh
git push --force-with-lease origin nombre-rama
```

`--force-with-lease` protege contra sobrescribir cambios remotos que no has visto. No lo sustituyas por `--force`.

## Borrar ramas remotas

Sólo por petición explícita:

```sh
git push origin --delete nombre-rama
```

Comprueba que no sea la rama predeterminada/protegida y que el trabajo esté integrado o respaldado.

## Tags

```sh
git tag --list
git show vX.Y.Z
git push origin vX.Y.Z
```

Borrar un tag publicado reescribe una referencia compartida:

```sh
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
```

Hazlo sólo si el usuario pide corregir ese tag y conoce el impacto. Si el nombre de versión ya existe para un release inmutable, preferir subir versión.

## Forks GitHub

Con `gh`, `gh repo sync` puede sincronizar el destino desde su parent por defecto y usa fast-forward salvo `--force`. No uses `--force` si una sincronización normal es suficiente.

```sh
gh repo sync
gh repo sync owner/fork --branch main
```
