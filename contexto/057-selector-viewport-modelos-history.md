# 057 · Selector compacto con viewport para modelos e historial

Fecha: 2026-07-28

## Decisión

El selector compacto probado temporalmente en `/models` y `/history` se adopta
como implementación definitiva. La variante anterior basada en tarjetas y en
un recorte manual del conjunto visible se retira de estas dos pantallas.

`/config` conserva su diseño de tarjetas: este cambio sólo estandariza los
selectores de listas largas.

## Componente compartido

Se incorpora `internal/tui/viewport_selector.go`. El componente:

- usa `bubbles/viewport` como región física de resultados;
- entrega al viewport el conjunto completo de resultados;
- calcula la altura disponible restando cabecera, búsqueda, separador, error y footer;
- cambia únicamente `YOffset` para mantener el elemento enfocado visible;
- usa una fila resaltada con `>` como foco, sin una caja independiente por resultado;
- comparte el mismo ancho útil entre búsqueda y lista;
- trunca contenido largo en una sola línea en lugar de romper la geometría.

Este enfoque replica la estrategia que ya funciona en el chat: el alto
restante pertenece a un viewport real, no a una ventana manual de elementos.

## `/models`

Cada resultado ocupa una sola fila:

`Proveedor · modelo · contexto`

El modelo activo conserva la marca `[ACTIVO]`. Se mantiene la búsqueda fuzzy
por modelo y proveedor, `↑/↓` para navegar, `Enter` para elegir y `Esc` para
volver.

## `/history`

Cada conversación usa dos filas compactas:

1. título;
2. antigüedad y número de turnos.

Se conservan búsqueda, `↑/↓`, `Enter`, `Ctrl+D` para borrar y `Esc` para volver.

## Limpieza

- Se elimina `internal/tui/selection_surface.go` y sus pruebas porque ya no tiene consumidores.
- Se elimina del historial el commit fallido `cd536bc` que intentaba corregir el conteo manual de filas.
- Se conserva `8fb7832` porque además contiene la política independiente de salida exclusiva mediante `/exit`.

## Validación

Con Go 1.24+:

```bash
go test ./internal/tui
go test ./...
go vet ./...
go build ./cmd/li
```

Prueba manual: abrir `/models` y `/history` en terminales de varias alturas,
recorrer hasta el último elemento y confirmar que el viewport aprovecha toda
la región disponible sin perder el foco.
