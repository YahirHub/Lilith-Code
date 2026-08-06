# 128. Verificar el mapeo `script_id → URL` por contenido

## Resultado de la segunda batería de pruebas

La repetición de las pruebas confirmó que `search_source` ya funciona con IDs
vigentes. El defecto restante estaba en el inventario devuelto por `scripts`:
un ID podía anunciar la URL de `app-shell.js` y, al buscar dentro de ese mismo
ID, devolver contenido perteneciente a `theme-init.js`.

Esto hacía que la búsqueda funcionara técnicamente, pero inducía al agente a
inspeccionar o atribuir el archivo equivocado.

## Causa técnica considerada

`Debugger.scriptParsed` entrega juntos el ID, la URL declarada, el contexto de
ejecución y el hash SHA-256 del código. Durante cambios rápidos de documento,
reconstrucciones de contextos o scripts repetidos en mundos aislados, el
inventario local puede terminar conservando metadatos que ya no describen el
contenido que CDP devuelve para ese ID.

Validar únicamente que el ID exista no detecta ese cruce: tanto
`Debugger.searchInContent` como `Debugger.getScriptSource` pueden aceptar el ID,
aunque la URL guardada localmente sea la de otro registro.

## Corrección

- `scripts` verifica por defecto cada ID mediante `Debugger.getScriptSource`.
- Se calcula el SHA-256 de la fuente real y se compara con el hash emitido por
  `Debugger.scriptParsed`.
- Si el hash coincide, el mapeo queda marcado como verificado.
- Si el hash pertenece de forma única a otro registro del inventario, se
  reasignan la URL y metadatos de fuente al ID correcto.
- La URL originalmente asociada se conserva como `reported_url` para auditoría.
- Si la fuente real no puede asociarse de forma única, `url` queda vacío y la
  antigua asociación sólo aparece como `reported_url`; así Lilith no presenta
  como cierta una URL no verificable.
- Los IDs que CDP invalida durante la verificación se eliminan del inventario.
- Cada documento lleva una generación interna. Si una navegación o recarga ocurre mientras se verifican las fuentes, el resultado completo se descarta para no aplicar metadatos del documento anterior sobre el nuevo.
- Errores individuales de lectura quedan en `verification_error` sin ocultar el
  resto de scripts.
- Se exponen el contexto de ejecución, tipo de contexto, frame, source map,
  condición de módulo y presencia de `sourceURL`, lo que permite distinguir
  scripts de la página, mundos aislados, módulos y fuentes nombradas.
- Se añadió `verify=false` para obtener deliberadamente un listado rápido sin
  descargar las fuentes. La verificación continúa activada por defecto.
- La respuesta incluye `verified_count` y cada registro informa
  `mapping_verified` y `mapping_source`.

## Pruebas añadidas

- Parseo de `executionContextAuxData` para contexto, frame y contexto principal.
- Compatibilidad del SHA-256 con el valor producido por V8.
- Reconciliación de dos IDs cuyos metadatos URL/hash quedaron cruzados.
- Protección contra URLs falsas cuando no existe una asociación única.
- La prueba opcional con Chrome real continúa verificando que `scripts` y
  `search_source` operen sobre el documento actual.

## Archivos modificados

- `internal/browser/types.go`
- `internal/browser/manager.go`
- `internal/browser/actions.go`
- `internal/browser/manager_test.go`
- `internal/tools/browser.go`

## Validación manual recomendada en Windows

```powershell
go test -mod=readonly -tags=grammar_set_core -count=1 -timeout=15m ./...
go run .\cmd\li\main.go
```

Prueba funcional:

```text
browser scripts session_id=jsecure
```

Comprobar que:

1. `verified_count` coincide con los registros que muestran
   `mapping_verified: true`.
2. El ID que contiene `theme-init.js` anuncia esa misma URL.
3. `search_source` sobre ese ID devuelve las coincidencias de tema.
4. Después de navegar o recargar se vuelve a ejecutar `scripts` antes de usar
   los IDs nuevos.

Para una página excepcionalmente grande se puede comparar rendimiento con:

```text
browser scripts session_id=jsecure verify=false
```

Ese modo es sólo diagnóstico rápido y deja explícitamente el mapeo sin verificar.
