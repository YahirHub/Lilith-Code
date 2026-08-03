# 100 — Inteligencia de código estática y contextual

## Objetivo

Añadir a Lilith una capa interna que adapte el trabajo del agente al sistema
operativo, el lenguaje y las herramientas reales del proyecto, sin depender de
CGO, procesos auxiliares obligatorios ni gramáticas descargadas en runtime.

## Decisión de arquitectura

Se creó `internal/codeintel` como paquete independiente de la TUI. El mismo
`Manager` se comparte entre el chat principal y los subagentes que usan la misma
raíz; los worktrees reciben un manager propio para no mezclar índices.

No se incorporó `tree-sitter.wasm`. En su lugar se usa
`github.com/odvcencio/gotreesitter`, un runtime Tree-sitter escrito en Go puro.
Los builds oficiales aplican `grammar_set_core`, que incorpora dentro del
binario el conjunto Core100 de tablas comprimidas. Se conserva así
`CGO_ENABLED=0`, compilación cruzada y un solo ejecutable autocontenido.

## Componentes implementados

### Detección

- Windows, Linux, WSL, Android/Termux, SSH, contenedores, distribución,
  arquitectura, shell y PATH.
- Herramientas instaladas y administradores de paquetes disponibles.
- Manifests de Go, Rust, Node/TypeScript/Deno, Python, PHP/Laravel, Godot,
  Maven, Gradle, CMake, Make, Ruby, Swift, Dart, Elixir y .NET.
- Lenguaje principal, frameworks conocidos, package manager y señal de
  monorepo.
- Servidores LSP e `index.scip` ya presentes.

### Índice sintáctico persistente

- Ruta: `~/.li/codeintel/<hash-raíz>/index.json`, visible mediante
  `code_intel_status`.
- Escaneo perezoso; el perfil de prompt no provoca indexación completa.
- Refresco incremental por tamaño, mtime, lenguaje y SHA-256.
- Límites de archivos/tamaño y exclusión de directorios generados. Un directorio
  fuente legítimo como `cmd/build` no se confunde con el `build/` generado en la
  raíz.
- Escritura temporal y reemplazo seguro en Unix/Windows.
- Refresco transaccional: una cancelación o fallo no modifica el índice vivo ni
  elimina archivos aún no visitados.
- Tree-sitter extrae definiciones y llamadas mediante helpers rápidos y queries
  de tags; un fallback estructural cubre gramáticas no incluidas o archivos no
  reconocidos.
- Los archivos Go se enriquecen con `go/parser` y `go/ast`: se indexan funciones,
  métodos, tipos, structs, interfaces, constantes y variables, junto con el path
  canónico del paquete, alias de imports y contenedor de cada referencia.

### Mapa y contexto

- Búsqueda de símbolos por nombre, nombre calificado, tipo y ruta.
- En Go, las referencias se resuelven contra la identidad canónica del símbolo y
  los alias importados; una consulta `version.Current` no puede coincidir con un
  método distinto llamado `current`. El fallback por nombre sólo se usa cuando
  la sintaxis no ofrece identidad calificada.
- Grafo conectado de archivos, paquetes, módulos/imports, declaraciones, llamadas,
  referencias y tests. La consulta elige semillas y después expande hasta dos
  saltos antes de filtrar, evitando grafos vacíos por perder los extremos de una
  relación.
- Ranking semántico bilingüe por consulta, símbolos, referencias, cambios de Git y
  relevancia de ruta. Para tareas de implementación prioriza código de producción
  y limita tests, documentación de `contexto/` y scripts de release salvo que se
  soliciten explícitamente.
- Fragmentos con números de línea delimitados alrededor de declaraciones, con
  límites por archivo para reducir lecturas completas y consumo de tokens.

### Semántica opcional

- Cliente LSP stdio de vida corta para símbolos, definición, referencias, hover
  y diagnósticos.
- Sólo inicia servidores ya instalados; no descarga ni instala ninguno.
- Valida que el archivo consultado permanezca dentro de la raíz.
- Cierre acotado con `shutdown`, `exit`, espera y kill de respaldo.
- SCIP sólo consulta un índice existente mediante un CLI `scip` ya instalado.

### Validación por ecosistema

Adaptadores para Go, Rust, Node/TypeScript, Deno, Python, PHP/Laravel, Ruby,
Dart/Flutter, Swift, Elixir, .NET, Godot, Maven, Gradle, CMake y Make.
Seleccionan comandos según manifests, wrappers del proyecto y ejecutables
reales. En Windows nativo, un Makefile secundario se deshabilita cuando existe
un adaptador principal o contiene asignaciones/utilidades POSIX que `cmd.exe` no
puede ejecutar.

`code_validate` no instala dependencias ni edita intencionalmente el código,
pero se clasifica como mutante porque compiladores, tests y scripts del proyecto
pueden generar cachés o artefactos. `code_format_validate` exige rutas explícitas
para aplicar formato, rechaza rutas que escapen de la raíz incluso mediante
symlinks y nunca ejecuta `cargo fmt` sobre todo un workspace como sustituto de
un formato dirigido.

## Herramientas del agente

- `code_intel_status`
- `code_symbols`
- `code_references`
- `code_graph`
- `code_context`
- `code_validate`
- `code_format_validate`
- `code_semantic`
- `code_scip_search`

La selección perezosa de tools las activa según términos de proyecto, símbolos,
LSP, validación o SCIP. El prompt del agente recibe sólo un perfil compacto con
host, proyecto y capacidades detectadas. Ese bloque se fusiona únicamente con el
mensaje de sistema del chat/subagente: no crea un turno de usuario, no altera el
historial heredado y la operación es idempotente.

## Build y release

- Nueva dependencia: `github.com/odvcencio/gotreesitter v0.48.0`.
- El registro de gramáticas expone una fábrica `(source, language) -> TokenSource`,
  mientras el parser recibe `TokenSourceFactory` con firma
  `(source) -> (TokenSource, error)`. `internal/codeintel/syntax.go` adapta ambas
  APIs explícitamente y rechaza token sources nulos para evitar fallos de build
  o pánicos durante el análisis.
- `cmd/build`, Makefile, Termux y workflow usan `grammar_set_core`.
- La TUI compara raíces del workspace mediante `path/filepath.Clean` directamente; no depende de helpers privados de `internal/subagents`, evitando errores de compilación entre paquetes.
- La conversión WinRT de `HSTRING` copia memoria mediante `RtlMoveMemory` y evita
  aritmética `uintptr -> unsafe.Pointer`, por lo que `go vet` para Windows deja de
  fallar en el código OCR preexistente.
- El workflow descarga módulos, ejecuta tests con el tag y verifica un build
  `CGO_ENABLED=0` sin dependencias dinámicas inesperadas.
- Versión elevada a `0.2.0` por tratarse de una capacidad mayor.

## Pruebas añadidas

`internal/codeintel/codeintel_test.go` cubre:

- detección de proyecto Go;
- persistencia y recarga del índice;
- símbolos, contexto, referencias y grafo;
- actualización/eliminación incremental;
- cancelación transaccional;
- rechazo de escapes de ruta;
- framing JSON-RPC, solicitudes del servidor y diagnósticos publicados por LSP;
- detección de SCIP;
- parsing correcto de renames de Git y nombres con espacios;
- validación Python por AST sin generar `__pycache__`;
- rechazo de escapes mediante symlinks;
- protección contra formato Rust global implícito;
- compatibilidad de la fábrica de tokens del registro de gramáticas con la API
  `gotreesitter.TokenSourceFactory`, incluida la respuesta nula;
- indexación de constantes Go y resolución de referencias canónicas con alias;
- ausencia de falsos positivos por diferencia de identidad/capitalización;
- ranking de recuperación de red por encima de documentación coincidente;
- construcción de un grafo de flujo con una arista real `calls`;
- exposición de la ruta física del índice;
- descarte de Makefiles POSIX secundarios en Windows;
- conservación de `cmd/build` frente al filtro de directorios generados;
- fusión idempotente del perfil de inteligencia en el mensaje de sistema sin
  mutar el historial padre;
- reutilización del manager de inteligencia de código cuando la TUI recibe rutas
  equivalentes después de `filepath.Clean`.

## Riesgos y límites

- Los LSP externos siguen dependiendo de que el usuario ya tenga un servidor compatible. `gopls` no se incrusta ni se instala para conservar el binario único/estático; sin él, Go usa un fallback interno para definiciones, referencias, hover de declaración y diagnósticos sintácticos.
- SCIP es una capa opcional y no genera índices.
- El conjunto Core100 aumenta el tamaño del ejecutable, pero evita archivos
  externos y conserva la distribución estática.
- El grafo sintáctico sigue siendo heurístico para llamadas mediante
  interfaces/variables, pero ya no crea fan-out por nombre entre paquetes:
  sólo conecta una llamada no calificada cuando existe un destino local único,
  o un selector cuando su alias de importación resuelve un paquete único.
  LSP/SCIP siguen siendo las capas de mayor precisión.
- Los archivos mayores a 1 MiB y repositorios con más de 25 000 archivos fuente
  se limitan para proteger memoria y latencia.

## Validación objetivo

```bash
gofmt -w internal/codeintel internal/tools internal/subagents internal/tui cmd/build
go test -tags=grammar_set_core ./...
go vet -tags=grammar_set_core ./...
CGO_ENABLED=0 go build -tags=grammar_set_core ./cmd/li
go run ./cmd/build build
```
