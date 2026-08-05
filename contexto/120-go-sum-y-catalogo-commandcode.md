# 120 — Manifiestos Go y catálogo completo de CommandCode

## Fecha

2026-08-05

## Objetivo

Sincronizar `go.sum` con el resultado de `go mod tidy` usado por el workflow y
resolver de forma explícita la ventana de contexto de todos los modelos que el
endpoint `/models` de CommandCode expone actualmente.

## Problema detectado

El workflow ejecuta `go mod tidy -diff`. El archivo `go.sum` del repositorio no
incluía los hashes completos de dependencias usadas por pruebas y conservaba
entradas obsoletas del grafo anterior, por lo que el paso terminaba con código 1.

Además, varios IDs nuevos sólo recibían `DefaultMaxContext` (128 000 tokens) o
heredaban accidentalmente una coincidencia parcial. Eso hacía que `/models`, la
barra de contexto y la compactación trabajaran con una ventana incorrecta.

## Cambios

- `go.sum` se reemplazó por el contenido normalizado generado por Go 1.25.12.
- `internal/models/catalog.go` incorpora IDs exactos para las familias nuevas de
  Anthropic, OpenAI, Qwen, StepFun, Gemini, Sakana, Thinking Machines, Meta y
  variantes rápidas de Kimi, GLM, HY3 y Nemotron.
- Los IDs con prefijo de proveedor siguen resolviéndose mediante `Normalize`.
- Se añadieron límites de salida cuando el proveedor los publica y son útiles
  para evitar solicitudes inválidas.
- Claude Sonnet 4/4.5 queda en 200k; Sonnet 4.6 y las familias Opus/Fable/Sonnet
  actuales de 1M se registran por separado para no heredar límites históricos.
- Una prueba exhaustiva cubre los 50 IDs entregados por CommandCode y exige una
  coincidencia explícita, nombre visible y contexto exacto.

## Contextos relevantes

- Claude Sonnet 5, Sonnet 4.6, Fable 5, Opus 5, Opus 4.8 y Opus 4.7: 1M.
- Claude Haiku 4.5: 200k.
- GPT 5.x incluidos: 400k.
- Qwen 3.8 Max, Qwen 3.7 Max/Plus/Flash y Qwen 3.6 Plus: 1M.
- Qwen 3.6 Max Preview: 262 144.
- Step 3.7/3.5 Flash: 256k.
- Gemini 3.x incluidos: 1 048 576.
- Fugu Ultra, Inkling e Inkling Small: 1M.
- Muse Spark 1.1: 1 048 576.

## Archivos modificados

- `go.sum`
- `internal/models/catalog.go`
- `internal/models/catalog_test.go`
- `contexto/000-contexto-maestro.md`
- `contexto/120-go-sum-y-catalogo-commandcode.md`
- `AGENTS.md`

## Pruebas requeridas

1. Ejecutar `go mod tidy -diff`; no debe producir diferencias.
2. Ejecutar `go test -mod=readonly -tags=grammar_set_core ./internal/models ./internal/providers`.
3. Abrir `/models` con CommandCode y verificar que ninguno de los 50 IDs muestre
   el contexto genérico de 128k salvo `poolside/laguna-s-2.1-free`, cuyo límite
   real sí es 128k.
4. Seleccionar modelos de 200k, 400k, 1M y 2M y comprobar la barra inferior y el
   umbral de compactación.
