# Tarea 08 — Componentes reutilizables para ajustes, /providers y /login

## Estado
en-proceso

## Objetivo
Crear un pequeño kit TUI reutilizable para pantallas de configuración y aplicarlo a `/providers` y `/login`, sustituyendo el listado de texto inútil de `/providers` por una pantalla interactiva con teclado y ratón.

## Criterios de aceptación
- `/providers` abre una pantalla real y no imprime texto en el chat.
- La pantalla permite seleccionar/activar proveedores, abrir `/models`, agregar un proveedor y eliminar proveedores personalizados con confirmación.
- Los proveedores personalizados exponen un switch de streaming persistente; los bundled muestran el control deshabilitado.
- Los controles reutilizables admiten teclado y clic izquierdo mediante el mouse ya habilitado en Bubble Tea.
- Existen componentes reutilizables para botón, switch, slider, stepper numérico y campo multilínea autoajustable.
- Los campos autoajustables crecen sólo hasta un máximo configurado, sin reservar altura vacía.
- `/login` usa el mismo lenguaje visual y botones clicables; el formulario personalizado usa el campo autoajustable para modelos/contexto.
- El diseño se adapta a terminales estrechas sin depender de coordenadas fijas de pantalla.
- No se agregan dependencias nuevas.
- Se agregan pruebas mínimas de interacción, persistencia y crecimiento de controles.

## Restricciones
- No tocar los cambios previos no relacionados de `README.md` ni `cmd/build/main.go`.
- Mantener una sola tarea `en-proceso`.
- Preservar compatibilidad con Bubble Tea v1.2.4 y Bubbles v0.20.0 ya fijados por el proyecto.

## Implementación actual
- Kit reutilizable en `internal/tui/settings_components.go`: cards, botones, grupos responsivos, switches, sliders, steppers, shell de input y textarea autoajustable.
- `/providers` ahora abre un administrador interactivo con selección, activación, cambio a `/models`, alta, eliminación confirmada y preferencia de streaming.
- `/login` comparte cards/botones/input shell; el alta OpenAI-compatible usa textarea autoajustable para modelos/contexto y mantiene consulta automática a `/models`.
- `/config` adopta el mismo switch y lenguaje visual para que el estándar no quede aislado a dos pantallas.
- El catálogo bundled de OpenCode Free se cachea una vez por proceso para evitar que cada interacción de `/providers` vuelva a bloquear por red.
- No se inventaron preferencias numéricas nuevas sólo para mostrar slider/stepper: ambos componentes quedan listos y probados para la primera opción numérica real.

## Validación del sandbox
- `go test ./internal/providers` correcto en copia temporal con Go 1.23.
- `gofmt` aplicado a todos los archivos Go modificados.
- Suite TUI completa pendiente porque el sandbox no puede descargar Go 1.24 ni las dependencias Charm fijadas por el proyecto.
- Se revisaron las APIs exactas de Bubble Tea v1.2.4 y Bubbles v0.20.0 usadas para mouse y textarea.
