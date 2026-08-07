# Flujo de implementación frontend

## 1. Inventario

Antes de editar:
- identifica framework/rendering (SSR, SPA, templates server-side);
- encuentra rutas/páginas/layouts/componentes compartidos;
- encuentra CSS/tokens/design system ya usados;
- revisa páginas vecinas para mantener patrones;
- localiza tests frontend existentes.

No crees un segundo sistema de componentes si el proyecto ya tiene uno.

## 2. Reproducir

Si es bug:
- abre la ruta real;
- registra viewport/estado relevante;
- snapshot del DOM accesible;
- consola `errors_only`;
- network `errors_only`;
- reproduce la interacción exacta.

Distingue bug visual, DOM/estado, API, red, auth/permisos y backend.

## 3. Implementar

Haz el cambio más pequeño que preserve:
- navegación;
- loading/error/empty states;
- teclado/foco;
- responsive;
- permisos/auth;
- contratos API existentes.

Reutiliza componentes/tokens existentes antes de introducir otros.

## 4. Validación local

Ejecuta formatter/lint/typecheck/build/tests apropiados. No sustituyen al navegador.

## 5. Browser verification

Vuelve a la UI real. Para una sola ruta puedes verificar en el contexto principal. Para múltiples páginas o auditoría de regresión, delega `frontend-browser-auditor`.

Valida al menos:
- ruta abre sin error;
- contenido principal visible;
- interacción modificada funciona;
- no aparecen errores nuevos de consola;
- requests críticas no fallan;
- navegación de entrada/salida sigue funcionando.
