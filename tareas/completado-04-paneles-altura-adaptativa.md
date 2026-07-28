# Tarea 04 — Paneles con altura adaptativa

## Estado
completado

## Objetivo
Hacer que todos los paneles visuales del transcript crezcan únicamente según el contenido visible, conservando como límite máximo la altura de vista previa que ya tenían.

## Alcance
- Panel de razonamiento (`ThinkingPanel`).
- Paneles de edición/escritura (`FilePanel`).
- Panel de comandos (`CommandPanel`), validando que mantenga el mismo comportamiento adaptativo y límite máximo.
- Mantener los modos expandido/preview y los atajos existentes.
- No eliminar contenido ni cambiar el historial del chat.

## Criterios de aceptación
- Un panel con pocas líneas no reserva filas vacías.
- Al llegar nuevas líneas durante streaming el panel crece gradualmente.
- La vista previa nunca supera su máximo histórico: razonamiento 6 líneas, archivos 12 líneas y comandos 10 líneas de salida.
- Al superar el máximo, se conserva la ventana de últimas líneas y el aviso de contenido oculto.
- El modo expandido sigue mostrando todo el contenido.
- Existen pruebas de regresión para alturas pequeñas y máximas.

## Implementado
- [x] `ThinkingPanel` deja de rellenar hasta 6 filas y crece sólo con el razonamiento visible.
- [x] `FilePanel` deja de rellenar hasta 12 filas y conserva 12 como máximo de preview.
- [x] `CommandPanel` usa la misma regla de límite adaptativo para su salida de hasta 10 filas.
- [x] El aviso de líneas ocultas ahora reserva explícitamente una fila y reporta el conteo real.
- [x] El modo expandido continúa mostrando todo el contenido.
- [x] Pruebas de regresión añadidas para los tres tipos de panel.

## Validación realizada
- `gofmt`: correcto en los archivos modificados.
- `git diff --check`: correcto.
- `cappedTailPreview`: smoke test aislado con Go local, correcto.
- `go test ./internal/tui`: no ejecutable en este sandbox porque el proyecto requiere Go 1.24 y las dependencias Charm no están disponibles localmente; la red del sandbox no puede obtenerlas.

## Validación local pendiente
```powershell
go test ./...
go vet ./...
go run .\cmd\li\
```

Probar reasoning corto/largo, `write_file`/`str_replace` con 1–12+ líneas y comandos con 0–10+ líneas de salida.
