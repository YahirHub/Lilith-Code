# 131. Reintentar error transitorio `reasoning_content` del proveedor

## Problema observado

Algunos gateways OpenAI-compatible pueden responder temporalmente con HTTP 400
mientras una ruta/upstream está saturada:

```text
HTTP 400: ... The `reasoning_content` in the thinking mode must be passed back to the API.
```

En la práctica el mismo request puede funcionar al reintentarse. Lilith ya
reintentaba silenciosamente respuestas HTTP 408, 429 y 5xx, pero cualquier 400
se consideraba terminal y llegaba inmediatamente a la TUI como
`Error del proveedor`.

## Corrección

- La clasificación de errores transitorios reconoce de forma deliberadamente
  estricta únicamente el HTTP 400 que contiene simultáneamente:
  - `reasoning_content`;
  - `thinking mode`;
  - `must be passed back`.
- Ese error entra en el mismo presupuesto existente de reintentos de servicio:
  hasta 3 intentos totales con espera progresiva.
- Los reintentos de servicio permanecen silenciosos: no se emite `Chunk.Err` ni
  un estado visual de reintento mientras haya intentos disponibles.
- Si un intento posterior funciona, la conversación continúa normalmente sin
  mostrar el 400 intermedio.
- Si los 3 intentos fallan, se conserva el error final para no ocultar una caída
  persistente.
- Cualquier otro HTTP 400 sigue siendo terminal y no se reintenta, evitando
  esconder errores reales de modelo, credenciales o payload.

## `go.sum`

Se completa `go.sum` con los checksums obtenidos en la validación de Windows,
incluidos los hashes completos de dependencias transitivas que faltaban y los
checksums requeridos por el grafo de pruebas. No se cambia la versión v0.3.1 ni
se introduce una dependencia nueva.

## Pruebas

Se añadieron pruebas que verifican:

1. El mensaje real de `reasoning_content` se clasifica como transitorio.
2. Un HTTP 400 genérico continúa sin ser reintentable.
3. Una secuencia `400 reasoning_content -> 200 SSE` realiza dos requests,
   entrega la respuesta correcta y no filtra ningún error/estado de reintento a
   la TUI.

La suite completa de `internal/providers/openai` también se ejecutó de forma
aislada en el entorno disponible, ajustando únicamente la directiva Go en una
copia temporal para poder usar Go 1.23.2; el repositorio entregado conserva
`go 1.25.12` sin modificaciones.

## Archivos

- `go.sum`
- `internal/providers/openai/client.go`
- `internal/providers/openai/client_transport_test.go`
- `contexto/131-reintentar-reasoning-content-transitorio.md`
