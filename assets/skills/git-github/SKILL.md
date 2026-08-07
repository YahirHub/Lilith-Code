---
name: git-github
description: Use for Git and GitHub repository work: inspect history, create/amend/remove commits, branch/merge/rebase/cherry-pick, sync remotes, push safely, manage pull requests/releases/workflows with gh, search repository content, remove files or code, and recover from mistakes.
user-invocable: true
model: inherit
argument-hint: "[operación Git/GitHub concreta]"
when_to_use: |
  Any task involving local Git history, branches, commits, remotes, GitHub CLI, pull requests, releases, Actions, repository cleanup, remote synchronization, force-with-lease, or recovery with reflog.
---

# Git + GitHub — índice operativo

Esta skill usa divulgación progresiva. **No leas todos los módulos.** Lee sólo los que correspondan a la operación solicitada mediante `skill_read`.

## Regla de entrada

Antes de mutar un repositorio:

1. Confirma la raíz real del repo y ejecuta/inspecciona `git status --short --branch`.
2. Preserva cambios ajenos al objetivo; no hagas `reset --hard`, `clean`, rebase destructivo ni force-push para "dejar limpio" un árbol que ya estaba modificado.
3. Inspecciona el historial/remotos necesarios antes de decidir cómo cambiarlo.
4. Para búsquedas de código local usa primero `code_search`; no instales `rg` sólo para buscar.
5. Git y `gh` son ejecutables externos: comprueba su disponibilidad antes de depender de ellos.

## Enrutador de módulos

Lee **sólo** lo necesario:

| Necesidad | Recurso |
|---|---|
| status, diff, add, commit, amend, restore, stash, ramas | `references/local-basics.md` |
| eliminar/reordenar commits, reset, revert, rebase, cherry-pick, reflog | `references/history-rewrite-recovery.md` |
| fetch/pull/push, upstream, tags, ramas remotas, force-with-lease | `references/remotes-sync.md` |
| GitHub CLI: auth, repos, PRs, merge, Actions, releases | `references/github-cli.md` |
| buscar/eliminar archivos, símbolos o contenido y publicar el cambio | `references/repository-search-cleanup.md` |
| conflictos de merge/rebase/cherry-pick y recuperación segura | `references/conflicts.md` |
| operación remota destructiva o historia publicada | `references/safety.md` |

Ejemplo: para "elimina los dos últimos commits locales y empuja la rama" lee `history-rewrite-recovery.md`, `remotes-sync.md` y `safety.md`; no cargues GitHub Actions ni releases.

## Política de seguridad

- Si un commit **ya fue publicado/compartido**, preferir `git revert` para conservar historia.
- Reescribir historia publicada sólo cuando el usuario lo pida de forma inequívoca y después de comprobar rama/remoto. Usar `--force-with-lease`, nunca `--force` como opción normal.
- No borrar repositorios, releases, tags remotos, ramas protegidas, secrets ni Actions artifacts sin una petición explícita para ese objeto.
- Antes de una operación destructiva sobre historia, registra el SHA actual (`git rev-parse HEAD`) y explica la ruta de recuperación mediante reflog/backup branch cuando sea pertinente.
- No cambies `user.name`/`user.email` globales. Respeta la identidad Git existente del repo salvo instrucción explícita.
- No expongas tokens; para `gh` usa sesión ya autenticada o mecanismos seguros del entorno.

## Cierre obligatorio

Después de una mutación:

1. Verifica `git status --short --branch`.
2. Inspecciona el diff/commit final relevante.
3. Ejecuta pruebas del proyecto si la modificación afecta código y están disponibles.
4. Si hubo push, confirma qué rama/tag/SHA quedó publicado; no asumas que el remoto aceptó la operación.
