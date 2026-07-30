# Tarea 13 — Resolución de modelos de subagentes

## Objetivo
Corregir la selección de modelo de los subagentes para que `model: default` use el proveedor/modelo seleccionado por el usuario en `/models`, conservar los modelos explícitos y aceptar una lista ordenada de modelos separados por comas.

## Alcance
- [x] `default` hereda el proveedor/modelo del turno padre.
- [x] `inherit` mantiene la compatibilidad existente y hereda el modelo padre.
- [x] Un modelo explícito sigue teniendo prioridad sobre el modelo padre.
- [x] `model` puede contener varios candidatos separados por comas y usa el primer candidato resoluble en orden.
- [x] El override `model` de la tool `Agent` acepta la misma sintaxis.
- [x] Mantener la compatibilidad con aliases Claude (`sonnet`, `opus`, `haiku`, `fable`).
- [x] Añadir pruebas de regresión para `default`, modelos explícitos y listas.
- [x] Actualizar documentación de agentes y `contexto/`.

## Estado
Implementado y validado con pruebas dirigidas. La suite completa del repositorio queda para un entorno con Go 1.24+ porque el sandbox actual sólo dispone de Go 1.23.2 y no puede descargar la toolchain ni módulos faltantes.

## Validación realizada
- `gofmt`: OK en los archivos Go modificados.
- Laboratorio con los archivos reales de `internal/agents`: `go test ./internal/agents` PASS y `go vet ./internal/agents` PASS.
- Laboratorio mínimo con las funciones exactas de resolución de `internal/subagents/runtime.go`: `go test` PASS y `go vet` PASS para `default`, modelo explícito y lista ordenada.
- Proyecto real: `go test ./internal/subagents`, `go test ./...` y `go vet ./...` intentados; bloqueados porque `go.mod` requiere Go 1.24.0 y el entorno tiene Go 1.23.2 sin acceso a `proxy.golang.org`.

## Criterios de finalización
- [x] Las pruebas específicas de resolución de modelos pasan en laboratorio aislado.
- [x] `go test ./...` y `go vet ./...` fueron intentados y la limitación del entorno quedó documentada.
- [x] No se altera la selección global de `/models`; el cambio sólo afecta a la resolución del modelo de subagentes.
