# Fecha
2026-07-27

# Objetivo
Analizar el snapshot de pi.dev aportado por el usuario y portar a Lilith las conductas relevantes de sus herramientas de edición y construcción de prompt para reducir reintentos, errores evitables y tokens reenviados, sin copiar el repositorio de referencia dentro del proyecto entregable.

# Decisiones tomadas
- La referencia se extrajo fuera del proyecto, en `/mnt/data/lab/pi/pi-main`; no forma parte del ZIP ni del repositorio Lilith.
- Se portó el principio central de `edit` de pi.dev: cada edición se valida contra el archivo actual en disco al momento de ejecutar. Se eliminó `Env.Seen` y la obligación ceremonial de llamar `read_files` antes de `str_replace` o `apply_diff`.
- `str_replace` admite varias ediciones no superpuestas contra el contenido original, acepta `edits` serializado accidentalmente como JSON string y tolera alias `oldText`/`newText` usados por pi.dev.
- El matching conserva exact-first y fallback fuzzy inspirado en pi.dev: normalización de saltos, Unicode NFKC, comillas, guiones, espacios especiales y whitespace final. Se preservan BOM UTF-8 y estilo CRLF/LF al escribir.
- `apply_diff` también valida el archivo actual directamente y preserva BOM/CRLF.
- Se mantiene una divergencia deliberada respecto a pi.dev: `write_file` NO puede sobrescribir archivos existentes. Esa política de Lilith protege contra reescrituras completas accidentales. Cuando el destino existe devuelve `FILE_EXISTS` como resultado recuperable y orienta a `str_replace`/`apply_diff`.
- Cuando un `write_file` es rechazado por `FILE_EXISTS`, se compactan sus argumentos en el historial enviado posteriormente al proveedor, porque el contenido nunca se aplicó y reenviar miles de tokens no aporta estado real al proyecto.
- Se portó el patrón `promptSnippet` + `promptGuidelines` de pi.dev. El system prompt sólo enumera reglas compactas de las herramientas activas; los contratos completos permanecen en sus schemas.
- El panel TUI de `str_replace` entiende `edits[]` y `edits` serializado como string para poder representar lotes multi-edición, en vez de mostrar únicamente el par simple `old/new`.

# Arquitectura actual
- `tools.Definition` contiene `PromptSnippet` y `PromptGuidelines`.
- `tools.PromptInfo` genera metadata compacta de las herramientas activas y deduplica guidelines.
- `str_replace` y `apply_diff` toman el lock por archivo, leen el estado actual, validan todas las mutaciones y sólo entonces escriben.
- `write_file` toma el mismo lock, crea únicamente destinos inexistentes y devuelve `FILE_EXISTS` sin mutar cuando ya existen.
- `ChatModel.runTools` detecta `FILE_EXISTS`; la respuesta de herramienta se conserva para que el modelo se autocorrija, pero la tool call rechazada se compacta antes de la siguiente petición.
- `systemPrompt` se construye a partir del set de herramientas activas y su firma entra en la caché del cálculo de contexto.

# Librerías usadas
- Librería estándar de Go para archivos, JSON, locks existentes y normalización de saltos.
- `golang.org/x/text/unicode/norm` para NFKC. El módulo `golang.org/x/text v0.30.0` ya estaba fijado en `go.mod`; ahora pasa a dependencia directa porque Lilith lo importa explícitamente.
- No se agregó ninguna librería nueva fuera de las dependencias ya presentes en el módulo.

# Archivos importantes modificados
- `internal/tools/registry.go`
- `internal/tools/files.go`
- `internal/tools/diff.go`
- `internal/tools/exec.go`
- `internal/tools/web.go`
- `internal/tui/chat.go`
- `internal/tui/filepanel.go`
- `go.mod`
- pruebas en `internal/tools/*_test.go` e `internal/tui/*_test.go`
- contexto histórico 007, 029 y 041 marcado como superado en lo relativo a `Env.Seen`.

# Problemas encontrados
- La guardia `Env.Seen` podía rechazar una edición segura aunque la herramienta ya pudiera comprobar por sí misma el contenido actual. Además introducía estado de sesión y posibles discrepancias de rutas en Windows.
- El modelo podía intentar `write_file` sobre un archivo existente, gastar tokens generando el contenido completo, recibir un error y después reenviar ese mismo payload en continuaciones posteriores.
- El contrato de edición era más rígido que pi.dev ante modelos que serializan `edits[]` como string.
- Las ediciones podían cambiar BOM o estilo de saltos de línea.
- El prompt repetía instrucciones extensas que ya viajan en el schema de cada herramienta.
- El panel de `str_replace` sólo visualizaba `old/new`, aunque el runtime soporta lotes `edits[]`.

# Soluciones implementadas
- Validación optimista segura sobre el contenido real del archivo en `str_replace` y `apply_diff`.
- Rechazo por mismatch, ambigüedad o solapamiento antes de escribir.
- Matching fuzzy alineado con pi.dev y preservación de BOM/CRLF.
- `FILE_EXISTS` recuperable, no destructivo y accionable.
- Compactación del payload rechazado de `write_file` en historial API.
- Prompt activo y corto basado en metadata de herramientas.
- Preview TUI multi-edición.
- Pruebas de no sobrescritura, edición sin read previo, JSON string, NFKC, BOM/CRLF, multi-edit y compactación de historial.

# Pendientes
- Ejecutar `go test ./...` y `go vet ./...` con Go 1.24 en el entorno Windows del usuario para cubrir la TUI completa. En el sandbox se validó `internal/tools` con un harness temporal Go 1.23 y una copia temporal del paquete `x/text` incluido en GOROOT, porque el sandbox no puede descargar el toolchain Go 1.24 ni módulos desde `proxy.golang.org`.
- Validar en uso real que modelos distintos cambien de `write_file` a `str_replace`/`apply_diff` tras `FILE_EXISTS` sin entrar en bucle.

# Próximos pasos
1. Probar una tarea sobre un archivo ya existente que antes provocaba `write_file` y comprobar que el panel queda `skipped` y el agente continúa editando.
2. Probar `str_replace` directamente sin `read_files` con un `old` válido; debe aplicar el cambio.
3. Forzar un `old` obsoleto o ambiguo; debe rechazar la mutación y pedir una lectura actual, sin tocar el archivo.
4. Ejecutar la suite completa en Windows y, si pasa, marcar la tarea 06 como completada.
