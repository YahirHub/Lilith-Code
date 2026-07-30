# Completado 18 · Corrección de espacio en Tcell Windows

- [x] Identificar la regresión en la traducción `tcell.EventKey` → `tea.KeyMsg`.
- [x] Conservar el rune de espacio junto con `tea.KeySpace`.
- [x] Mantener intacto el pegado atómico y el render completo por celdas.
- [x] Añadir una prueba de regresión específica para la barra espaciadora.
- [x] Documentar causa raíz, alcance y pruebas manuales.
