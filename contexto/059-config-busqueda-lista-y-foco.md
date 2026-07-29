# 059 · Configuración de búsqueda por lista y foco explícito

Fecha: 2026-07-28

## Decisión

La configuración de motores web queda centralizada exclusivamente en
`/config > Búsqueda`. Se elimina `/setup-search` y sus alias para evitar dos
rutas públicas que resuelven la misma tarea.

La barra superior `General / Búsqueda / Seguridad` pasa a ser una región de
foco real. Las teclas `←/→` sólo cambian de sección cuando el foco está en esa
barra. Al pulsar `↓`, el foco entra en el contenido de la sección; mientras el
foco esté abajo, las teclas horizontales quedan disponibles para los controles
de esa sección y nunca cambian accidentalmente la pestaña superior. Al subir
desde el primer control se vuelve a la barra.

## Lista principal de Búsqueda

La pantalla principal de `Búsqueda` deja de renderizar todos los proveedores y
todas las acciones simultáneamente. Ahora muestra una lista compacta similar a
`/history` con los siete motores:

- Tavily;
- Brave Search;
- Exa;
- Linkup;
- Firecrawl;
- SerpApi;
- Zenserp.

Cada fila muestra su estado. Un proveedor con API key configurada usa el color
de éxito (verde). Los estados principales son `SIN CONFIGURAR`, `CONFIGURADO`
y `ACTIVO`. La segunda línea aclara si está deshabilitado, pendiente de validar,
con error de validación o si además es el predeterminado.

`↑/↓` mueve la selección y `Enter` o clic abre el proveedor seleccionado.

## Pantalla de proveedor

Cada proveedor se configura en una pantalla secundaria propia. Allí se conservan
las mismas capacidades introducidas en el cambio 058:

- configurar o reemplazar la API key;
- probar la conexión;
- habilitar/deshabilitar;
- usar como motor predeterminado;
- eliminar la API key;
- ordenar respaldos;
- probar todos los motores configurados.

`Esc` regresa a la lista de motores. Los subflujos de API key y orden de respaldo
regresan a la pantalla del proveedor, no a otra sección de `/config`.

## Disponibilidad para el agente

Este cambio es únicamente de navegación/configuración. No modifica la regla de
seguridad del cambio 058: `web_search` sólo existe para el agente cuando hay al
menos un motor con credencial, validación correcta y estado habilitado.

## Archivos principales

- `internal/tui/config_screen.go`;
- `internal/tui/config_search.go`;
- `internal/tui/config_search_view.go`;
- `internal/tui/settings_components.go`;
- `internal/tui/commands.go`;
- `internal/tui/config_screen_test.go`.

## Validación manual

1. Abrir `/config` y confirmar que el foco inicia en la barra superior.
2. Usar `←/→` para cambiar entre General, Búsqueda y Seguridad.
3. En Búsqueda pulsar `↓`; mover la lista y confirmar que `←/→` ya no cambia de sección.
4. Subir hasta el primer proveedor y pulsar `↑`; confirmar que el foco vuelve a la barra superior.
5. Configurar un proveedor y confirmar que su nombre queda verde; tras una validación correcta y habilitada debe mostrar `ACTIVO`.
6. Abrir un proveedor con `Enter` o clic y comprobar todas sus acciones sin saturar la lista principal.
7. Confirmar que `/setup-search`, `/search-setup` y `/search` ya no aparecen como comandos de configuración.
