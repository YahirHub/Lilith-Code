# Seguridad para operaciones Git/GitHub destructivas

## Clasificación

### Normalmente seguras
- status/log/diff/show/fetch;
- crear ramas;
- commit local de cambios solicitados;
- push normal a la rama objetivo ya confirmada.

### Requieren cuidado explícito
- amend/rebase/reset de historia local;
- borrar ramas/tags locales;
- merge de PR;
- push a ramas compartidas.

### Destructivas/remotas
- `reset --hard` con datos no protegidos;
- `push --force*`;
- borrar rama/tag remoto;
- borrar repo/release;
- `gh api -X DELETE`;
- saltarse branch protection con permisos admin.

## Checklist antes de reescribir remoto

1. `git status --short --branch`.
2. `git rev-parse HEAD`.
3. `git branch -vv` y `git remote -v`.
4. `git fetch <remote>`.
5. Verificar que la rama remota no avanzó inesperadamente.
6. Preferir `git push --force-with-lease`, especificando remoto y rama.
7. Confirmar el SHA remoto después del push.

## Backup temporal

Cuando haya riesgo real de perder commits:

```sh
git branch backup/antes-de-cambio <sha>
```

No publiques esa rama automáticamente si contiene material sensible; el backup local/reflog suele ser suficiente.

## Cambios ajenos

Nunca descartes, stages, commits o empujes cambios preexistentes que no pertenecen a la tarea sólo para conseguir un árbol limpio.
