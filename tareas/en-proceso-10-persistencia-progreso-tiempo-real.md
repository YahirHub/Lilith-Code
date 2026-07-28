# Tarea 10 — Persistencia del progreso en tiempo real

## Objetivo
Evitar que Ctrl+C, un cierre rápido o una interrupción de la CLI pierdan el razonamiento, respuesta parcial y progreso visual de herramientas del turno actual.

## Alcance
- Guardar el historial API en cada frontera semántica segura.
- Mantener un checkpoint ligero del progreso mutable durante streaming.
- Forzar un checkpoint ligero del turno al cancelar con Ctrl+C, sin reescribir todo el historial.
- Recuperar el transcript visible sin contaminar el protocolo de tool calls.
- Mantener compatibilidad con sesiones antiguas.

## Estado
En proceso.
