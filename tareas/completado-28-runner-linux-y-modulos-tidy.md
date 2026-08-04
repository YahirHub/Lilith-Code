# Tarea 28 · Runner Linux y manifiestos Go ordenados

Estado: completado

## Objetivo

Corregir el fallo de `go mod tidy -diff`, eliminar el job Windows del workflow de release y ejecutar validación, compilación multiplataforma y publicación desde un único runner Ubuntu.

## Alcance

- Aplicar al repositorio el estado canónico producido por `go mod tidy`.
- Mantener `go mod tidy -diff` como guard de CI.
- Ejecutar pruebas y release en un solo job Linux.
- Compilar binarios Windows desde Linux con `CGO_ENABLED=0`.
- Compilar los tests de paquetes sensibles para Windows sin intentar ejecutarlos en Linux.
- Ejecutar el smoke test del instalador PowerShell bajo `pwsh` en Ubuntu.
- Documentar el límite: no se ejecutan pruebas nativas de PowerShell 5.1/CMD al retirar el runner Windows.

## Implementación

- Se aplicó el estado canónico de `go mod tidy` a `go.mod` y `go.sum`.
- Se reemplazaron los dos jobs por un solo runner Ubuntu.
- Se conservaron builds Windows mediante cross-compilation estática.
- Se añadió compilación de tests Windows y smoke test PowerShell portable.
- Se documentó la pérdida deliberada de ejecución nativa Windows en CI.

## Validación

- YAML parseado correctamente.
- `git diff --check` sin errores.
- Targets Windows presentes en el builder y cubiertos por prueba.
- El diff de módulos coincide con el resultado reportado por `go mod tidy -diff` en Go 1.24.
- La suite final queda a cargo de `test.cmd` local y del workflow real por falta de Go 1.24/red en el entorno de entrega.
