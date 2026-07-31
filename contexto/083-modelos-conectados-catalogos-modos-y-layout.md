# 083 · Modelos conectados, catálogos dinámicos, Goal y ancho del chat

## Fecha

2026-07-31

## Objetivos

1. Evitar que `/models` exponga modelos de proveedores cuya autenticación todavía no está disponible.
2. Actualizar automáticamente los catálogos de todos los proveedores conectados y conservar nuevos modelos.
3. Añadir Goal al ciclo primario de agentes junto con Build y Plan.
4. Corregir el desbordamiento derecho del input y del chrome inferior.
5. Dejar contexto suficiente para continuar desde un chat nuevo o desde Codex.

## Conexión de proveedores

Se añadió `internal/providers/connection.go` como única regla de disponibilidad:

- `bundled`, `none` y configuraciones antiguas sin `auth`: conectadas;
- `api_key`: exige una clave no vacía en `provider-auth.json`;
- `env`: exige que exista la variable configurada;
- `oauth`: exige access token o refresh token guardado.

`ConnectedProviders` conserva el orden original. `ReconcileActive` garantiza que la selección activa pertenezca a un proveedor conectado y a un modelo existente. `SetActive` rechaza explícitamente proveedores desconectados.

`/providers` sigue mostrando conexiones no iniciadas para permitir configurarlas, pero las marca como **SIN CONEXIÓN**, explica qué falta y deshabilita Activar.

## `/models`

El selector ahora:

- sólo construye filas con `ConnectedProviders`;
- muestra una explicación cuando no hay conexiones disponibles;
- actualiza catálogos en segundo plano al abrir;
- permite repetir la actualización con `Ctrl+R`;
- conserva la última caché si una API falla;
- presenta un resumen de catálogos actualizados o errores parciales.

Codex deja de aparecer antes de completar OAuth. Después de guardar la sesión, sus modelos son seleccionables.

## Descubrimiento automático de modelos

`RefreshConnectedModels` consulta en paralelo cada proveedor conectado:

- OpenAI-compatible/custom: `GET {baseURL}/models`, respetando API key, variable, bearer y headers personalizados;
- OpenCode Free: mismo endpoint, filtrando IDs terminados en `-free`;
- ChatGPT Codex: catálogo autenticado de cuenta con `client_version`, `originator`, bearer y `chatgpt-account-id`.

El parser admite catálogos en arrays o en `models`, `data`, `items` y `available_models`; reconoce IDs como `id`, `slug`, `model_slug`, `modelId` y `model_id`. También recupera nombre, contexto y límite de salida cuando la API los publica.

La respuesta remota es autoritativa, pero conserva metadata local que la API omita. Un proveedor fallido no interrumpe a los demás.

### Persistencia

- custom: catálogo actualizado dentro de `providers.json`;
- bundled: `provider-model-cache.json`.

Los bundled arrancan con fallback offline y luego superponen la última caché válida. Esto evita que un modelo recién descubierto desaparezca al volver al chat, recargar proveedores o reiniciar sin red.

## Build, Plan y Goal

Se amplió `plan.Mode` con `goal` y se mantiene serializable en sesiones.

Ciclo:

```text
Tab:       Build → Plan → Goal → Build
Shift+Tab: Build ← Plan ← Goal ← Build
```

Presentación:

- `build ❯` con acento principal;
- `plan ❯` con acento secundario;
- `goal ❯` con acento de éxito.

Un texto normal enviado en Goal:

1. permanece visible como mensaje del usuario;
2. se convierte en goal persistente;
3. inicia ejecución autónoma si no hay turno;
4. si ya hay trabajo, reorienta el mismo turno sin crear otro padre.

Plan conserva su política de sólo lectura y su handoff de una sola vez a Build. Goal usa las capacidades de implementación normales y no consume accidentalmente el handoff Plan → Build.

## Corrección de ancho

La causa del borde derecho cortado era que el sistema interno interpreta `Style.Width` como ancho del **contenido**. El input enviaba casi todo el ancho de terminal y después añadía dos bordes y dos columnas de padding, por lo que el resultado excedía la pantalla.

Se añadieron helpers comunes:

- `chatUsableWidth`: reserva la última columna para scrollbar;
- `chatBorderedContentWidth`: resta bordes y padding;
- `chatPaddedContentWidth`: resta padding.

Se aplicaron a:

- textarea/input;
- status bar;
- cola;
- actividad;
- paleta de comandos;
- TodoWrite;
- launcher de preguntas Plan.

El cálculo de altura del textarea usa ahora el ancho real del texto después del prompt, mejorando el wrap de `build ❯`, `plan ❯` y `goal ❯`.

## Contexto persistente

Se creó `AGENTS.md` en la raíz con:

- orden de lectura inicial;
- arquitectura vigente;
- invariantes de tview, proveedores, catálogos, modos y layout;
- seguridad;
- pruebas objetivo;
- autor y reglas de commits.

También se reescribió `contexto/000-contexto-maestro.md`, que contenía referencias obsoletas a OAuth placeholder, bash pendiente, Bubble Tea y funcionalidades ya implementadas.

## Pruebas de regresión

- OAuth oculto antes de login y visible después.
- Reconciliación de proveedor activo desconectado.
- Refresh custom con API key, headers, nuevos modelos y persistencia.
- Proveedor desconectado sin solicitudes de red.
- Catálogo Codex con query/header de cuenta y cache bundled.
- Carga de cache bundled tras reinicio.
- Selector `/models` sin Codex antes de OAuth.
- Persistencia/restauración de Goal.
- Ciclo Tab y Shift+Tab de los tres agentes.
- Texto normal de Goal convertido en objetivo durable.
- Input y status sin exceder la columna reservada en varios anchos.

## Pruebas manuales recomendadas

1. Sin iniciar Codex, abrir `/models` y confirmar que no aparece ningún modelo Codex.
2. Completar `/login`, abrir `/models` y esperar la actualización; confirmar que Codex aparece.
3. Añadir un modelo nuevo en un endpoint custom, pulsar `Ctrl+R` y comprobar que aparece sin reiniciar.
4. Desconectar la red y confirmar que el último catálogo sigue visible.
5. Pulsar Tab: Build → Plan → Goal → Build; probar Shift+Tab al revés.
6. En Goal, escribir un objetivo y confirmar que arranca la ejecución persistente.
7. Redimensionar Windows Terminal y verificar que el borde derecho del input queda visible antes de la scrollbar.
