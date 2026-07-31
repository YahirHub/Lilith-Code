# 084 — Catálogos manuales cuando `/models` no existe

## Objetivo

Permitir proveedores OpenAI-compatible que implementan inferencia pero no publican `GET {baseURL}/models`, sin mostrar falsos errores ni perder los modelos introducidos manualmente.

## Comportamiento

- `404 Not Found`, `405 Method Not Allowed` y `501 Not Implemented` se interpretan como **descubrimiento de modelos no soportado**.
- Esta condición no se agrega a `RefreshReport.Errors`.
- El catálogo manual o la última caché válida permanece intacto, incluido contexto y modelo activo.
- `/models` muestra un estado neutral indicando cuántos proveedores usan modelos manuales.
- Los demás proveedores continúan actualizándose en paralelo.
- Se vuelve a intentar en futuras aperturas o con `Ctrl+R`; así el proveedor podrá descubrir modelos si añade el endpoint más adelante.
- Errores de red, autenticación, JSON inválido o respuestas HTTP distintas siguen reportándose sin borrar la caché.

## Alta de proveedor personalizado

En el paso Modelos:

- El usuario puede escribir `modelo` o `modelo=contexto`, separados por coma o salto de línea.
- Si deja el campo vacío se intenta consultar `/models`.
- Cuando el endpoint no existe, el formulario permanece abierto y muestra una nota neutral para introducir los modelos manualmente; no usa el estilo de error ni bloquea el alta.

## Archivos principales

- `internal/providers/catalog_unavailable.go`
- `internal/providers/catalog.go`
- `internal/providers/catalog_refresh.go`
- `internal/tui/login_custom.go`
- `internal/tui/model_selector.go`

## Pruebas

Se cubren:

- detección tipada del catálogo ausente;
- conservación de modelos y metadata manuales durante refresh;
- ausencia de errores en el reporte;
- formulario custom desbloqueado y con instrucción neutral.

## Validación manual

1. Agregar un proveedor cuyo chat funcione pero cuya ruta `/models` responda 404.
2. Escribir al menos un modelo manual y guardar.
3. Abrir `/models`: el modelo debe aparecer y el encabezado no debe mostrar error.
4. Pulsar `Ctrl+R`: el modelo debe conservarse.
5. Comprobar que un 401/403 o JSON inválido continúe apareciendo como problema real de actualización.
