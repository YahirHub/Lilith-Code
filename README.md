# Lilith

Lilith (`li`) es una CLI agéntica escrita en Go que corre en tu terminal.
Habla con proveedores compatibles con la API de OpenAI, ejecuta herramientas
sobre tu repositorio y ofrece una TUI interactiva construida sobre tview/Tcell.

## Características

- TUI con historial, Markdown y selector de proveedores/modelos; `/models`
  aplica la selección a la siguiente petición sin reiniciar la CLI.
- Onboarding de primer arranque con tres rutas claras: proveedor personalizado,
  **ChatGPT Codex** mediante OAuth o continuar con los modelos gratuitos de
  OpenCode Free.
- Herramientas integradas para leer y editar archivos, buscar con ripgrep y
  ejecutar comandos en shell POSIX (BusyBox en Windows).
- Sesiones persistentes que pueden retomarse con `li --continue`.
- Transporte resiliente para VPS/SSH: si Internet o el proveedor se vuelven
  inaccesibles, conserva el turno, muestra un estado legible y reintenta
  automáticamente cuando la conexión regresa. Los streams incompletos se
  descartan antes de repetirlos para no duplicar texto ni tool calls.
- Compatibilidad con Termux ARM64 mediante compilación nativa en el dispositivo:
  el instalador clona el tag estable, instala Go con `pkg`, compila e instala en
  `$PREFIX/bin`.
- Sin dependencias externas obligatorias en runtime: la toolchain auxiliar se
  descarga y verifica mediante SHA-256 al primer uso; en Termux se usan los
  paquetes nativos del repositorio.
- Motor de inteligencia de código por proyecto: detecta sistema, shell,
  manifests, lenguajes y herramientas; mantiene un índice incremental de
  símbolos/referencias, construye contexto compacto, consulta LSP/SCIP cuando
  ya existen y selecciona validaciones reales por ecosistema.


## Inteligencia de código estática

Lilith incorpora `internal/codeintel`, compartido por el agente principal y los
subagentes. El motor trabaja de forma perezosa: la detección ligera se inyecta
en el prompt y el índice completo sólo se actualiza cuando una herramienta de
código lo necesita.

Incluye:

- detección de Windows, Linux, WSL, contenedores, SSH y Termux, arquitectura,
  distribución, shell, `PATH`, manifests, frameworks, monorepos, package manager
  y herramientas instaladas;
- análisis sintáctico Tree-sitter en Go puro con el conjunto Core100 de
  gramáticas comprimidas embebido dentro del ejecutable;
- fallback estructural para archivos cuyo lenguaje no esté incluido en el
  conjunto embebido;
- índice incremental persistente bajo `~/.li/codeintel/`, fuera del repositorio,
  cuya ruta física se muestra en `code_intel_status`;
- para Go, una capa adicional basada en `go/ast` que indexa funciones, métodos,
  tipos, structs, interfaces, constantes y variables con nombres canónicos de
  paquete;
- resolución de referencias Go por identidad calificada y alias de importación,
  evitando confundir símbolos distintos que comparten nombre;
- grafo conectado de paquetes, archivos, imports, declaraciones, llamadas y
  pruebas, con expansión alrededor de los nodos relevantes para la tarea;
- selección semántica bilingüe de fragmentos delimitados por declaraciones, que
  prioriza código de producción y reduce documentación/scripts irrelevantes;
- integración opcional con servidores LSP ya instalados para definiciones,
  referencias, hover y diagnósticos;
- consulta opcional de un `index.scip` existente mediante un CLI `scip` ya
  instalado;
- adaptadores de validación para Go, Rust, Node/TypeScript, Deno, Python,
  PHP/Laravel, Ruby, Dart/Flutter, Swift, Elixir, .NET, Godot, Maven, Gradle,
  CMake y Make; en Windows se omite un Makefile secundario con sintaxis POSIX
  cuando existe un adaptador nativo del proyecto.

No se descargan gramáticas, language servers, indexadores ni compiladores en
runtime. Los builds oficiales usan `CGO_ENABLED=0` y el tag
`grammar_set_core`, por lo que el ejecutable continúa siendo autocontenido.
Las herramientas disponibles para el modelo son `code_intel_status`,
`code_symbols`, `code_references`, `code_graph`, `code_context`,
`code_semantic`, `code_scip_search`, `code_validate` y
`code_format_validate`.

## Escritura segura de archivos

Lilith no necesita construir documentos largos mediante comandos de shell. El
agente dispone de herramientas nativas que reciben el contenido como argumentos
estructurados:

- `write_file`: crea un documento completo o reemplaza uno existente únicamente
  cuando se declara `overwrite=true`; puede validar el SHA-256 previamente leído;
- `append_file`: agrega una sección completa y acotada, útil para reportes largos;
  cada llamada estructurada se limita a 1 MiB y el documento puede crecer por
  secciones verificadas;
- `create_file`: conserva semántica estricta de archivo nuevo;
- `str_replace` y `apply_diff`: realizan cambios localizados sobre código existente.

Las escrituras completas se realizan con un temporal en el mismo directorio,
`fsync`, reemplazo atómico y verificación final de bytes y SHA-256. Una
cancelación no deja el archivo destino a medio escribir. En Windows se usa una
operación nativa de reemplazo; Linux y Termux mantienen la misma garantía sin
CGO.

`run_terminal_command` bloquea antes de ejecutar heredocs incompletos y comandos
de escritura inline demasiado largos. Esto evita que un límite del proveedor,
la TUI o el shell guarde un Markdown truncado. Para reportes extensos, el modelo
debe usar `write_file` una sola vez cuando el contenido cabe o `append_file` por
secciones, y revisar el conteo/hash devuelto al finalizar.

## Atajos principales

- `Enter`: enviar el mensaje o agregar steering durante una tarea.
- `Alt+Enter`: encolar un follow-up para después del trabajo actual.
- `Shift+Enter` o `Ctrl+Enter`: insertar una nueva línea.
- `Ctrl+C`: borrar todo el texto escrito en el input sin cancelar el turno ni
  eliminar mensajes ya encolados.
- `Esc`: cancelar el turno activo.
- `Alt+↑`: recuperar al editor los mensajes pendientes de la cola.
- `/exit`: cerrar Lilith de forma explícita.

## Instalación rápida

Los instaladores viven en la rama `main`, no dentro de los assets del release.
Así pueden corregirse sin publicar otra versión de los binarios.

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | bash
```

Termux ARM64:

```bash
pkg install -y curl
curl -fsSL https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/YahirHub/Lilith-Code/main/install.ps1 | iex
```

Los instaladores actualizan una versión anterior y conservan `~/.li`. Linux y
Windows descargan el binario del release y verifican SHA-256. Termux instala
`git`, `golang` y `ripgrep`, clona la versión estable y compila nativamente para
evitar incompatibilidades del ejecutable Android precompilado. Consulta
[`install.md`](./install.md) para más opciones.

## Releases

La versión se define en `internal/version/version.go`. Para publicar una nueva
versión, cambia `version.Current`, haz commit y ejecuta manualmente el workflow
**Publicar release** desde GitHub Actions. El workflow prueba el proyecto,
ejecuta `cmd/build`, genera checksums de los binarios y crea notas agrupadas con
los commits realizados desde el tag anterior. Los instaladores no se adjuntan al
release: siempre se descargan directamente desde el repositorio.
