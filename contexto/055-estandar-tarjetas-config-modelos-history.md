# 055 · Estándar de tarjetas para configuración, modelos e historial

Fecha: 2026-07-28

## Objetivo

Fijar la propuesta visual **Tarjetas** como estándar definitivo de `/config` y reutilizar sus primitivas en `/models` y `/history` sin duplicar layouts ni introducir componentes genéricos innecesarios.

## `/config`

- Se eliminaron las tres variantes experimentales restantes (`Lista compacta`, `Panel dividido`, `Secciones`).
- Tarjetas deja de ser un experimento y pasa a ser el único diseño de configuración.
- Se eliminó la tarjeta/lista que imprimía los nombres de todas las skills detectadas. El control `Skills` conserva únicamente el estado ON/OFF y el conteo total.
- La cabecera incorpora pestañas reutilizando `settingsButtonGroup`:
  - `General` (primera y predeterminada).
  - `Búsqueda` (motores de búsqueda).
  - `Seguridad`.
- `Búsqueda` y `Seguridad` muestran por ahora `Características en desarrollo` y no inventan opciones que aún no existen.
- `Tab`/`Shift+Tab` y `←`/`→` cambian de sección; `↑`/`↓` mueven el foco de las tarjetas interactivas.
- Se conserva soporte de clic y el borde completo de foco elegido por el usuario.

## Componente compartido de selección

Se agregó `internal/tui/selection_surface.go` para `/models` y `/history`.

El componente reutiliza las primitivas ya existentes de settings (`settingsInput`, `settingsCard`, `settingsHeader` y `settingsFooter`) y sólo resuelve lo que ambas pantallas necesitan en común:

- búsqueda a todo el ancho útil de la terminal;
- tarjetas de resultados alineadas al mismo ancho útil completo que la barra de búsqueda;
- borde completo sobre la tarjeta seleccionada;
- cálculo de ventana visible usando la altura real renderizada de cada tarjeta;
- estado vacío, footer y error comunes.

No se añadió un framework de componentes nuevo: es una capa pequeña sobre los componentes existentes, siguiendo la regla Ponytail de simplicidad y reutilización.

## `/models`

- Se sustituyó la lista plana por tarjetas de ancho completo, alineadas con la búsqueda.
- La barra de búsqueda ocupa el ancho útil completo de la terminal.
- Cada modelo ocupa una sola línea dentro de su tarjeta: `Proveedor · Modelo · Contexto`; ya no existen cabeceras separadas por proveedor.
- La búsqueda fuzzy sigue funcionando por ID de modelo y ahora también por nombre de proveedor.
- El modelo activo continúa identificado visualmente con `ACTIVO`.
- Se eliminó el emoji de lupa; la búsqueda usa texto (`Buscar`) y elementos TUI normales.

## `/history`

- Usa exactamente la misma superficie visual que `/models`.
- Cada conversación usa una tarjeta de ancho completo, alineada con la búsqueda, conservando título, antigüedad y cantidad de turnos.
- La búsqueda usa todo el ancho útil de la terminal y ya no muestra emoji.
- Se conservan `Enter` para reanudar, `Ctrl+D` para borrar y `Esc` para volver.

## Archivos principales

- `internal/tui/config_screen.go`
- `internal/tui/selection_surface.go`
- `internal/tui/model_selector.go`
- `internal/tui/history_screen.go`
- `internal/tui/config_screen_test.go`
- `internal/tui/selection_surface_test.go`
- `internal/tui/model_selector_test.go`

## Validación

Ejecutar con Go 1.24+:

```bash
go test ./internal/tui
go test ./...
go vet ./...
go build ./cmd/li
```

Además, validar manualmente en una terminal ancha y una estrecha que la búsqueda y las tarjetas compartan exactamente el mismo ancho útil, que los modelos permanezcan en una sola línea y que el borde de selección nunca desaparezca al navegar.


## Nota de evolución

El estándar de tarjetas sigue vigente para `/config`. El selector de tarjetas
que este documento introdujo para `/models` y `/history` fue sustituido más
tarde por el selector compacto basado en un viewport físico documentado en
`057-selector-viewport-modelos-history.md`.
