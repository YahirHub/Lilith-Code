# Conflictos de Git

## Principio

No "resuelvas" un conflicto escogiendo automáticamente ours/theirs sin entender qué comportamiento debe sobrevivir.

## Diagnóstico

```sh
git status
git diff --name-only --diff-filter=U
git diff
```

Lee las versiones base/local/remota cuando sea necesario. Busca tests o consumidores del código antes de combinar manualmente.

## Merge

Después de editar cada archivo:

```sh
git add -- archivo
git status
```

Continúa/commit cuando no queden `U`.

Para abandonar:

```sh
git merge --abort
```

## Rebase

```sh
git add -- archivo
git rebase --continue
```

Si la resolución elegida fue incorrecta o el contexto cambió:

```sh
git rebase --abort
```

## Cherry-pick

```sh
git add -- archivo
git cherry-pick --continue
# o
git cherry-pick --abort
```

## Validación

Tras cualquier conflicto:

- ejecutar tests del área afectada;
- revisar `git diff`/commit final;
- confirmar que no quedaron marcadores `<<<<<<<`, `=======`, `>>>>>>>`;
- verificar status limpio o sólo con cambios esperados.
