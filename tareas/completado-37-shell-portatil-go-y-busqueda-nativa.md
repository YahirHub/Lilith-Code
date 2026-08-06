# Estado
Completado

# Objetivo
Añadir una shell Bash/POSIX de respaldo escrita en Go y eliminar la dependencia obligatoria de ripgrep para búsquedas, conservando shells y herramientas externas existentes.

# Implementación
- Integración estática de `mvdan.cc/sh/v3` mediante `shell=portable`.
- Prioridad intacta para PowerShell, CMD, Bash, `sh` y ejecutables reales en `PATH`.
- Toolbox Go acotada para búsqueda, lectura y operaciones comunes de archivos.
- Motor `internal/textsearch` como fallback transparente de `code_search`.
- Cancelación, límites de memoria, salida truncada y errores explícitos para flags o ejecutables no soportados.
- Schemas, prompts, perfil de entorno, selección perezosa y `tool_search` actualizados.
- Instalador Termux sin `ripgrep` obligatorio.
- Pruebas unitarias añadidas y documentación técnica/licencias actualizadas.

# Resultado
Lilith conserva su binario Go estático y puede interpretar sintaxis Bash/POSIX y buscar código incluso en hosts mínimos. Los shells y aceleradores instalados siguen teniendo prioridad, mientras Git y demás programas externos nunca se simulan ni se anuncian como disponibles.

# Validación de entrega
- `gofmt`, `git diff --check` y análisis sintáctico de 293 archivos Go: correctos.
- `internal/textsearch`: pruebas y `go vet` correctos en módulo aislado.
- `internal/shell`: comprobación de tipos contra la API pública de `mvdan/sh`.
- Instalador Linux/Termux: simulaciones correctas.
- Suite integral pendiente de ejecución en un host con Go 1.25.12; el entorno de entrega tiene Go 1.23.2 sin descarga de toolchains.
