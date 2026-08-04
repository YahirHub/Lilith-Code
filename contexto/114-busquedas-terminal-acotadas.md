# Fecha

2026-08-04

# Objetivo

Evitar que búsquedas recursivas generadas por el agente recorran dependencias, artefactos y metadatos durante minutos, manteniendo sin límite implícito las compilaciones, pruebas e instalaciones.

# Decisiones tomadas

- Priorizar `code_search` en las instrucciones de `run_terminal_command` para búsquedas de código.
- Aplicar un timeout automático de 30 segundos únicamente a búsquedas de repositorio cuando el modelo no proporciona `timeout_seconds`.
- Considerar búsquedas de repositorio a `grep -r/-R`, `rg`, `ripgrep`, `find`, `fd`, `fdfind` y `git grep` cuando son comandos simples.
- Rechazar `grep -r/-R` sin operando de archivo antes de crear el proceso y orientar a `code_search` o a una ruta concreta.
- No modificar comandos con pipes, redirecciones, conectores, saltos de línea o rutas explícitas.
- Elevar la versión a `0.2.2` para publicar el arreglo.

# Arquitectura actual

`internal/shell/search.go` analiza comandos simples sin ejecutar un parser de shell completo. El guard de `shell.Run` rechaza el `grep` recursivo ambiguo antes de crear el proceso. `internal/tools/exec.go` decide el timeout por tipo de comando y muestra una sugerencia hacia `code_search` cuando una búsqueda alcanza el límite.

# Librerías usadas

No se agregaron ni actualizaron dependencias.

# Archivos importantes modificados

- `internal/shell/search.go`
- `internal/shell/search_test.go`
- `internal/shell/shell.go`
- `internal/tools/exec.go`
- `internal/tools/exec_test.go`
- `internal/version/version.go`
- `install.md`
- `AGENTS.md`
- `contexto/000-contexto-maestro.md`
- `contexto/114-busquedas-terminal-acotadas.md`
- `tareas/completado-31-acotar-busquedas-terminal.md`

# Problemas encontrados

- El agente podía elegir `grep -rn` en lugar de la herramienta especializada `code_search`.
- Un `grep` recursivo sin ruta explícita podía recorrer `.git`, dependencias y artefactos generados.
- Al omitir `timeout_seconds`, esas búsquedas heredaban la ejecución ilimitada diseñada para builds y tests.
- El panel sólo podía mostrar que el proceso seguía activo hasta que terminara o el usuario lo cancelara.

# Soluciones implementadas

- Se incorporó detección de búsquedas de repositorio sin afectar comandos largos no relacionados.
- Se añadió un rechazo preventivo únicamente para un `grep` recursivo simple sin destino.
- Se incorporó un límite automático de 30 segundos y una sugerencia explícita para reducir ruta/glob o usar `code_search`.
- Se añadieron pruebas de análisis, preservación de rutas explícitas, comandos complejos y rechazo antes de crear el proceso.

# Pendientes

- Ejecutar `test.cmd -Vet` en Windows con Go 1.24.
- Comprobar desde una sesión real que una búsqueda sin ruta muestra el ajuste y termina rápidamente.

# Próximos pasos

1. Ejecutar `test.cmd -Vet`.
2. Probar una búsqueda de código desde Lilith.
3. Publicar la release `v0.2.2` mediante el workflow manual.
