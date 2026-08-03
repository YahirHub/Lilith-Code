# 102 — Shells nativas, límites explícitos y semántica Go estática

## Objetivo

Completar los límites que todavía podían confundir al modelo y mejorar la
portabilidad de `run_terminal_command` sin perder la distribución de un solo
binario con `CGO_ENABLED=0`.

## Límites de archivo visibles

Los schemas, guidelines y resultados de las herramientas ahora aclaran:

- `write_file`: contenido completo de hasta 1 MiB (1,048,576 bytes) por llamada;
- `append_file`: hasta 1 MiB por sección y 64 MiB (67,108,864 bytes) como tamaño
  final;
- `append_file` concatena bytes exactamente y no inserta `\n` automáticamente;
- los documentos superiores a 1 MiB deben construirse mediante secciones
  semánticas completas, encadenando el SHA-256 anterior.

Los resultados de escritura incluyen los límites aplicables y, en append,
`newline_added: no`.

## Selección de shell

`run_terminal_command` incorpora `shell=auto|powershell|cmd|bash|sh`.

En `auto`:

- Windows prefiere PowerShell para comandos neutrales;
- detecta sintaxis PowerShell (`$env:`, cmdlets, variables `$...`);
- detecta sintaxis CMD (`%VAR%`, `set VAR=`, `dir`, `where`, `for /...`);
- detecta sintaxis POSIX (`VAR=value comando`, `$()`, `mkdir -p`, `rm`, `sed`,
  `/dev/null`) y usa Bash/sh sólo si existe;
- Linux, macOS y Termux prefieren Bash y usan `sh` como respaldo.

Si la sintaxis detectada requiere una shell ausente, la ejecución se rechaza con
un mensaje para reescribir el comando o elegir `shell` explícitamente. No se
intenta ejecutar el comando con otro intérprete. La salida de la tool call y el
panel TUI muestran la shell realmente usada.

Las redirecciones accidentales a `null`, `/dev/null`, `$null` o `NUL` se
normalizan al dispositivo correcto del intérprete seleccionado.

## `gopls` y binario estático

`gopls` permanece como ejecutable externo opcional. No se incrusta, descarga ni
instala porque eso convertiría la capacidad en otra distribución/herramienta y
no forma parte del binario único de Lilith.

Para Go sin `gopls`, `code_semantic` usa `builtin-go`, completamente estático:

- símbolos del documento desde el índice;
- definición y referencias mediante identidad canónica/alias de importación;
- hover con la declaración indexada y línea fuente;
- diagnósticos sintácticos mediante `go/parser`.

El fallback no se presenta como inferencia completa de tipos. Cuando `gopls`
está instalado, el LSP conserva prioridad.

## Precisión del grafo

Se retira el fan-out anterior por nombre para llamadas ambiguas. Una arista
`calls` no calificada sólo se crea si hay un destino callable único dentro del
paquete actual. Para selectores de paquetes, el alias importado debe resolver un
único símbolo. Las llamadas por interfaz/variable sin tipo conocido quedan sin
arista antes que inventar una relación plausible.

## Pruebas

Se agregan regresiones para:

- shell nativa predeterminada en Windows/Unix;
- detección PowerShell, CMD y POSIX;
- selección explícita y shell ausente;
- null device por shell;
- schema de `run_terminal_command` y visualización TUI;
- límites visibles de `write_file`/`append_file`;
- fallback semántico Go y diagnósticos;
- ausencia de fan-out ambiguo en `code_graph`.
