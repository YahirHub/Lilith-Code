# 003 · Tool calls con carga perezosa y corrección del input

## Motivo

Lilith no tenía tool calls: al pedirle "una web de gatitos" imprimía el HTML en
pantalla en vez de escribir el archivo. Además la caja de entrada se desbordaba
del ancho del terminal y mostraba tres prompts `❯` (uno por línea del textarea
de alto fijo 3).

## Decisiones

1. **Paquete `internal/tools`** con el registro de herramientas portado desde
   `original/packages/agent-runtime/src/tools/`:
   `read_files`, `write_file`, `str_replace`, `list_directory`, `glob`,
   `code_search` (ripgrep del toolchain), `run_terminal_command`
   (sobre `internal/shell`, portable en Windows), `read_url` y la meta
   herramienta `tool_search`.
2. **Carga perezosa de esquemas** (`tools.Select`), portada de
   `original/packages/agent-runtime/src/tools/lazy-tool-selection.ts`:
   - un saludo puro (`hola`, `gracias`) no envía **ningún** esquema;
   - por defecto sólo viaja `tool_search`;
   - patrones del prompt (escribir, buscar, rutas de archivo, comandos, URLs)
     activan sólo el subconjunto relevante;
   - el modelo materializa el resto en caliente llamando a `tool_search`.
   Con esto el coste de tokens por turno es mínimo y sigue siendo un CLI
   completo.
3. **Prompt de sistema mínimo**: dos frases, y la parte de herramientas sólo se
   añade cuando hay esquemas activos.
4. **Bucle de agente en la TUI**: historial real (`[]openai.Message`, incluidos
   los mensajes `role: tool`) separado del transcript visible; máximo 25 pasos
   de herramienta por turno.
5. **Modo `!comando`** ya ejecuta de verdad contra `internal/shell`.
6. **Seguridad**: toda ruta se resuelve dentro del directorio del proyecto; se
   rechazan rutas absolutas y `..`. Lecturas y salidas acotadas a 128 KB.

## Correcciones de UIX

- El textarea arranca con alto 1 y crece hasta 8 líneas según el contenido
  real (`syncInputHeight`), así desaparecen los `❯` sobrantes.
- La caja de entrada se renderiza con `Width(w-2)` y el textarea con `w-4`,
  compensando bordes y padding: ya no se desborda del terminal.
- El viewport recalcula su alto a partir del alto real del input.

## Pendiente

Modo seguro con confirmación previa para herramientas mutantes
(`write_file`, `str_replace`, `run_terminal_command`), según
`original/docs/safe-mode.md`.