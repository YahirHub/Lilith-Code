# Consola, red y carga

## Consola

Captura errores después de iniciar la sesión. Para errores de carga inicial, limpia y recarga/navega de nuevo antes de leer `console errors_only`.

No trates warnings cosméticos como equivalentes a exceptions que rompen UI. Prioriza:
- uncaught exceptions;
- rejected promises;
- errores de módulos/assets;
- CSP relevante;
- hydration/render mismatch;
- loops de errores.

## Network

Revisa `network errors_only` y requests críticas. Diferencia:
- 4xx por auth/validación esperada;
- 404 de asset/endpoint;
- 5xx backend;
- CORS/CSP;
- aborted/canceled esperado por navegación/zoom;
- timeout/latencia excesiva.

Usa `response_body` sólo para requests recientes; CDP puede descartar bodies antiguos.

## Scripts

Después de cada navegación/reload, `script_id` puede cambiar. Ejecuta `scripts` otra vez antes de `search_source`. Usa el mapeo verificado por hash salvo que necesites explícitamente inventario rápido.

## Performance

`performance` sirve para detectar señales grandes (memoria/navigation/resources), no para afirmar una optimización sin baseline. Para problemas de payload, inspecciona tamaño/frecuencia de requests concretas.
