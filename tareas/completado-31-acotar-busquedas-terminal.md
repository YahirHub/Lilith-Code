# Tarea 31 · Acotar búsquedas de terminal

## Estado

Completada y validada.

## Objetivo

Evitar que búsquedas recursivas generadas mediante `run_terminal_command` recorran directorios pesados sin límites o permanezcan ejecutándose indefinidamente cuando el agente omite una ruta explícita.

## Alcance

- Detectar búsquedas de repositorio ejecutadas con grep/rg/find/git grep.
- Acotar automáticamente `grep -r/-R` sin destino explícito al directorio actual y excluir carpetas generadas comunes.
- Aplicar un timeout seguro por defecto únicamente a búsquedas de repositorio.
- Orientar al agente hacia `code_search` y rutas/globs concretos.
- Añadir pruebas y documentación.
