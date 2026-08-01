# 089 — Goal sin límites artificiales y prevención de archivos `null`

## Problemas corregidos

1. Goal podía detenerse con `budget_limited` al alcanzar un presupuesto opcional de tokens y mostraba `Goal detenido: presupuesto agotado`.
2. Un proveedor podía enviar el argumento `path` como el texto literal `null`; la herramienta lo interpretaba como un nombre válido y creaba un archivo `null` en la raíz del proyecto.

## Goal sin límites artificiales

Se eliminó el presupuesto de tokens de la API y del comando de Goal:

- `/goal` ya no anuncia `--tokens` ni `--token-budget`.
- `create_goal` ya no expone `token_budget` en su schema.
- `State` ya no persiste `TokenBudget`.
- `AddUsage` conserva tokens aproximados sólo como métrica y nunca cambia el estado.
- No existe detención por cantidad de tokens, pasos, continuaciones, turnos ni tiempo.
- Un error real del proveedor finaliza únicamente la petición actual y se muestra al usuario; no transforma el goal en un estado duradero de límite.
- Las únicas transiciones que detienen la continuación autónoma son pausa/cancelación explícita, `blocked`, `complete` o eliminación del goal.

Por compatibilidad, si una sesión antigua contiene `budget_limited` o `usage_limited`, `NewManager` la migra a `active`. La sintaxis antigua `/goal --tokens N objetivo` se acepta de forma transitoria, ignora el valor y muestra que el goal es ilimitado, evitando romper sesiones o hábitos existentes.

Los campos `TokensUsed` y `TimeUsedSeconds` se mantienen para diagnóstico y para `/goal status`; no son umbrales.

## Prevención de rutas placeholder

`internal/tools.resolve` es ahora la validación central para todas las herramientas que leen o modifican rutas. Antes de resolver contra el proyecto:

- recorta espacios externos;
- rechaza rutas vacías;
- rechaza bytes NUL;
- rechaza los placeholders completos `null`, `./null`, `undefined`, `nil`, `<nil>` y `(null)` sin distinguir mayúsculas.

La validación ocurre antes de `PreflightCreateFile`, `os.MkdirAll` y `os.WriteFile`, y también protege `read_files`, `str_replace`, `apply_diff` y OCR porque comparten `resolve`.

Además, `internal/shell` corrige el otro origen común del archivo basura: comandos generados con `> null`, `2> null`, `>> "NULL"` o redirecciones equivalentes. Como Lilith siempre ejecuta un shell POSIX —BusyBox/Git Bash también en Windows— esos destinos se normalizan a `/dev/null` antes de iniciar el proceso. No se modifican nombres como `null.txt`, rutas `src/null/...` ni texto ordinario que contenga la palabra.

No se bloquea una ruta legítima que contenga esa palabra dentro de otra ruta, por ejemplo `src/null/decoder.go`; sólo se rechaza cuando el argumento completo representa el placeholder.

## Pruebas

- Uso extremadamente alto no detiene un Goal activo.
- Estados persistidos antiguos de límite migran a activo.
- `create_file` rechaza todas las variantes placeholder y deja el directorio completamente vacío.
- El estado completado sigue deteniendo correctamente su propio loop.

## Prueba manual recomendada

1. Ejecutar `/goal tarea larga` y comprobar `/goal status`: debe indicar `Sin límites artificiales`.
2. Dejar correr varias continuaciones y confirmar que nunca aparece `presupuesto agotado`.
3. Ejecutar una llamada de prueba a `create_file` con `path: "null"`; debe devolver un error explícito y no crear ningún archivo.
4. Abrir una sesión antigua que hubiera quedado en `budget_limited`; debe reanudarse como activa.
