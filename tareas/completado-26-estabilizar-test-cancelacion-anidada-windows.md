# Tarea 26 · Estabilizar test de cancelación anidada en Windows

Estado: completado

## Objetivo

Eliminar el falso negativo `nested child did not start` sin debilitar la comprobación del árbol de cancelación padre-hijo.

## Implementación

- Señal de arranque síncrona al entrar al stream hijo.
- Diagnóstico de terminación prematura y cantidad de requests.
- Temporizadores separados para arranque y cancelación.
- Repetición de la regresión cinco veces en GitHub Actions Windows.

## Validación

- `gofmt` aplicado.
- `git diff --check` sin errores.
- Workflow validado sintácticamente.
- La suite completa debe ejecutarse con Go 1.24 y dependencias oficiales mediante `test.cmd` o GitHub Actions.
