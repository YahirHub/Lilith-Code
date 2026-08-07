# Buscar, eliminar y publicar cambios en un repositorio

## Búsqueda local

Usa primero herramientas nativas de Lilith:

- `code_search` para texto/símbolos.
- `glob` para patrones de rutas.
- `read_files` para contexto preciso.
- `git ls-files` para inventario tracked cuando sea útil.

Ejemplos Git auxiliares:

```sh
git grep -n "texto"
git ls-files '*patron*'
git status --short
```

No uses búsquedas destructivas (`find ... -delete`, reemplazos masivos) antes de listar exactamente los targets.

## Eliminar archivos o código

1. Encuentra todas las referencias.
2. Distingue archivos generados/vendor de fuente real.
3. Elimina/modifica sólo targets confirmados mediante herramientas de archivo de Lilith cuando sea posible.
4. Repite la búsqueda para demostrar que la referencia desapareció.
5. Ejecuta build/tests/lint relevantes.
6. Revisa `git diff --stat` y `git diff`.
7. Stage/commit sólo los cambios del objetivo.

Para un archivo tracked que debe desaparecer puedes usar herramientas de archivo y luego stagear, o `git rm -- path` si Git está disponible. No uses `git clean -fdx` como mecanismo de limpieza general.

## Buscar/inspeccionar en GitHub sin clonar

```sh
gh search code "texto" --repo OWNER/REPO
gh search commits "texto" --repo OWNER/REPO
gh repo read-dir [DIRECTORY] -R OWNER/REPO
gh repo read-file PATH -R OWNER/REPO
```

`gh search code` usa la búsqueda de código disponible mediante la API de GitHub y puede diferir de la búsqueda web; úsala para localizar candidatos y confirma el archivo/contenido antes de mutar.

Si la tarea implica modificar el repo, normalmente es más seguro clonar/usar el checkout existente, cambiar localmente, probar, commit y push. La API remota directa para borrar archivos debe ser la excepción porque omite muchas verificaciones locales. Si el usuario pide expresamente una mutación remota directa, inspecciona primero SHA/rama/repo y usa el endpoint documentado mediante `gh api`; nunca inventes el SHA ni borres sobre una rama no confirmada.

## Publicar la limpieza

```sh
git status --short --branch
git diff --check
git diff --staged
git commit -m "Eliminar ..."
git push
```

Si la rama remota no existe usa `git push -u origin <rama>`.

## Verificación remota

Tras push:

```sh
git rev-parse HEAD
git ls-remote --heads origin <rama>
```

Cuando corresponda, usa `gh pr view`/`gh pr checks` para verificar la integración. Nunca declares que el remoto cambió sólo porque el comando local de commit tuvo éxito.
