# 082 · Compatibilidad de reasoning inline y OCR estructural local

## Fecha

2026-07-31

## Objetivos

1. Evitar que el razonamiento de modelos como MiniMax aparezca mezclado dentro
   de la respuesta final cuando el proveedor lo devuelve en `content` mediante
   etiquetas como `<think>...</think>`.
2. Ampliar la compatibilidad con variantes de razonamiento usadas por gateways
   OpenAI-compatible y servidores locales.
3. Dar a los modelos sin visión una herramienta local para extraer texto de
   imágenes conservando, hasta donde permite OCR, posiciones y estructura de
   interfaces.
4. Mantener el binario de Lilith compilable con `CGO_ENABLED=0`.

## Compatibilidad de razonamiento

El cliente OpenAI-compatible ya leía `reasoning_content`, `reasoning` y parte de
`reasoning_details`, pero enviaba `delta.content` directamente al chat. Por eso
MiniMax podía mostrar literalmente:

```text
<think>
...
</think>
respuesta final
```

Se añadió un parser incremental independiente del transporte SSE. Conserva un
buffer mínimo para reconocer delimitadores partidos entre chunks de red y
separa cada fragmento en `Chunk.Thinking` o `Chunk.Delta`.

### Formatos estructurados reconocidos

- `reasoning_content`
- `reasoning`
- `thinking`
- `analysis`
- `thought`
- `reasoning_details[].text`
- `reasoning_details[].summary`
- `reasoning_details[].content`

`reasoning_details[].data` se considera contenido cifrado/de continuidad y no
se muestra como texto de pensamiento.

### Marcadores inline reconocidos

- `<think>...</think>`
- `<thinking>...</thinking>`
- `<analysis>...</analysis>`
- `<reasoning>...</reasoning>`
- `<thought>...</thought>`
- `[THINK]...[/THINK]`
- `[ANALYSIS]...[/ANALYSIS]`
- `[REASONING]...[/REASONING]`
- canales Harmony `analysis` → `final`

La comparación de marcadores es insensible a mayúsculas y conserva índices de
bytes válidos aun cuando haya Unicode antes de la etiqueta.

### Reglas de emisión

- funciona tanto en streaming como en respuestas no streaming;
- las etiquetas nunca llegan al texto final visible;
- `ThinkingDone` se emite al comenzar la respuesta final, una tool call o al
  finalizar el stream;
- si un gateway entrega simultáneamente razonamiento estructurado y el mismo
  bloque inline, se conserva una sola copia;
- una etiqueta de apertura sin cierre permanece como pensamiento en lugar de
  filtrarse a la respuesta final.

## Herramienta `extract_image_text`

Se añadió una herramienta no mutante y local para capturas, mockups, documentos
escaneados y fotografías con texto.

### Backend Windows

En Windows usa `Windows.Media.Ocr` mediante llamadas WinRT escritas en Go puro.
No enlaza DLL de OCR durante la compilación y no requiere CGO. Se recupera cada
palabra con su rectángulo original en píxeles.

La capa WinRT fue adaptada de `zn-chen/sysocr`, bajo licencia MIT. La atribución
y el texto de licencia están en `internal/imageocr/NOTICE.md`.

### Fallback multiplataforma

Si el backend nativo no está disponible, busca `tesseract` primero mediante el
directorio de herramientas de Lilith y después en `PATH`. Solicita salida TSV,
que contiene palabras, confianza y bounding boxes. Tesseract es un proceso
externo opcional: no se enlaza al ejecutable de Lilith y, por tanto, no cambia
la compilación estática del binario principal.

En Windows la herramienta se ofrece siempre por el backend del sistema. En
Linux/macOS aparece cuando `tesseract` está instalado.

### Salidas

La herramienta acepta:

- `format=layout` (predeterminado): representación pensada para LLM;
- `format=text`: sólo orden de lectura;
- `format=json`: resultado completo y cajas.

`layout` genera:

1. mapa monoespaciado aproximado de la posición de cada palabra;
2. regiones probables de encabezado, navegación lateral, contenido y pie;
3. separadores horizontales/verticales detectados con análisis de bordes en Go;
4. texto en orden de lectura;
5. lista de palabras con coordenadas porcentuales y confianza.

El mapa se mantiene entre 48 y 140 columnas. El texto de lectura se acota para
no saturar el contexto y la tabla de coordenadas muestra hasta 500 bloques; el
formato JSON conserva todos los bloques.

### Seguridad y límites

- La imagen nunca se sube desde la herramienta.
- El texto OCR se marca como contenido no confiable para evitar que comandos o
  instrucciones presentes dentro de una imagen se ejecuten automáticamente.
- OCR no equivale a visión: no identifica con fiabilidad iconos sin etiqueta,
  colores semánticos, fotografías u objetos sin texto.
- La estructura es aproximada; combina bounding boxes y separadores, no una
  reconstrucción pixel-perfect del DOM o del diseño original.
- Windows debe tener al menos un paquete de idioma OCR disponible. En el
  fallback, Tesseract debe tener instalados los idiomas solicitados.

## Selección automática

La selección perezosa activa `extract_image_text` cuando el prompt contiene:

- rutas `.png`, `.jpg`, `.jpeg`, `.gif`, `.bmp`, `.tif`, `.tiff` o `.webp`;
- términos como imagen, captura, screenshot, OCR, mockup, interfaz o UI.

`read_files` ya no recomienda un visor externo para imágenes: indica usar la
nueva herramienta para obtener OCR, estructura y coordenadas.

## Archivos principales

- `internal/providers/openai/client.go`
- `internal/providers/openai/reasoning_parser.go`
- `internal/providers/openai/client_reasoning_test.go`
- `internal/imageocr/`
- `internal/tools/image_ocr.go`
- `internal/tools/image_ocr_test.go`
- `internal/tools/registry.go`
- `internal/tools/files.go`
- `contexto/082-compatibilidad-reasoning-inline-y-ocr-estructural.md`

## Pruebas añadidas

### Reasoning

- MiniMax con `<think>` y ambas etiquetas divididas entre chunks SSE;
- variantes XML, corchetes y Harmony;
- texto Unicode antes de una etiqueta;
- alias estructurados y `reasoning_details.content`;
- exclusión de datos cifrados;
- deduplicación entre campos estructurados y bloque inline;
- bloque sin cierre;
- respuesta no streaming.

### OCR

- parseo de TSV con dimensiones, confianza y cajas;
- orden de lectura por líneas;
- mapa espacial y coordenadas porcentuales;
- detección de un divisor vertical en una imagen sintética;
- fallback inyectable sin enlazar OCR nativo;
- registro y selección automática de la herramienta;
- prueba manual con Tesseract real sobre una UI sintética.

## Validación requerida en el equipo objetivo

1. Probar MiniMax en streaming y confirmar que el panel de pensamiento reciba el
   contenido interno y el chat sólo la respuesta final.
2. En Windows, ejecutar `extract_image_text` sobre una captura PNG/JPEG real y
   confirmar que `Windows.Media.Ocr` encuentre el idioma instalado.
3. Probar una interfaz con sidebar, encabezado y formulario para revisar mapa,
   regiones, separadores y coordenadas.
4. Compilar con Go 1.24 real y `CGO_ENABLED=0` para Windows y Linux.
