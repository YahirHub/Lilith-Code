# Tarea 32 — Normalizar argumentos compatibles de str_replace

## Estado

completado

## Objetivo

Evitar que modelos entrenados con otros agentes fallen repetidamente al llamar
`str_replace` con nombres de campos compatibles como `old_string`/`new_string`,
y endurecer el schema para que siempre solicite una edición real además de la ruta.

## Alcance

- Normalizar alias conocidos en el par simple y en `edits[]`.
- Mantener la seguridad: el texto objetivo debe seguir siendo no vacío y único.
- Mejorar schema, mensajes de error y vista previa TUI.
- Añadir pruebas de runtime, schema y panel.
- Elevar la versión para publicar el arreglo.


## Implementado

- [x] Schema obliga a proporcionar una edición además de la ruta.
- [x] Alias Claude/Pi normalizados en runtime y `edits[]`.
- [x] Reemplazo omitido rechazado sin borrar contenido.
- [x] Panel y compactación histórica reconocen los mismos campos.
- [x] Pruebas de regresión añadidas.
- [x] Versión elevada a 0.2.3.
