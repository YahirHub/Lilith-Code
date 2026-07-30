# Completado 19 · Corrección de pegados largos en Tcell Windows

- [x] Identificar la presión de eventos causada por compartir captura y render.
- [x] Separar el drenado físico de Tcell en una goroutine dedicada.
- [x] Mantener el acumulador de bracketed paste dentro del puente de entrada.
- [x] Entregar cada pegado como un único `tea.KeyMsg` atómico.
- [x] Preservar la corrección de espacio y los demás eventos de entrada.
- [x] Añadir una prueba de regresión con 5,000 caracteres y salida bloqueada.
- [x] Documentar causa raíz, arquitectura y pruebas manuales.
