# 040 — Login con modelos y contexto opcionales

# Fecha

2026-07-27

# Objetivo

Simplificar el alta de proveedores compatibles desde `/login`: el usuario puede
escribir manualmente los modelos y, si lo necesita, sobrescribir el contexto con
la sintaxis `modelo=contexto`. Si deja el campo vacío y pulsa Enter, Lilith
consulta `GET {baseURL}/models`, usa los identificadores devueltos y termina el
alta automáticamente.

# Decisiones tomadas

- Se conserva un único campo para modelos/contexto para evitar agregar pasos y
  estado innecesario al asistente.
- La entrada manual sigue aceptando `modelo` y `modelo=contexto`, separados por
  coma o salto de línea.
- El contexto es opcional. Cuando no se declara, `providers.Enrich` resuelve el
  valor mediante el catálogo local y aplica el valor por defecto existente a
  modelos desconocidos.
- Enter con el campo vacío inicia `GET {baseURL}/models` usando la API key si
  existe. Al recibir una lista válida se persiste inmediatamente el proveedor;
  ya no se obliga a confirmar una segunda vez la lista descubierta.
- El endpoint de modelos se mantiene en el formato compatible actual:
  `{"object":"list","data":[{"id":"..."}]}`. No se presupone que `/models`
  publique la ventana de contexto, porque ese dato no forma parte del objeto
  básico que consume Lilith.
- Se reutilizó `finish` para que la ruta manual y la ruta descubierta compartan
  exactamente la misma persistencia y recarga del proveedor.

# Arquitectura actual

```text
/login
  -> CustomLoginModel
     -> modelos manuales -> ParseModelsInput -> finish -> Upsert
     -> Enter vacío      -> FetchModels GET /models -> finish -> Upsert

contexto omitido
  -> Enrich
     -> catálogo local conocido
     -> DefaultMaxContext para modelo desconocido
```

# Librerías usadas

No se añadieron dependencias. Se mantiene `net/http` para descubrimiento y los
componentes Bubble Tea/Bubbles ya presentes en la TUI.

# Archivos importantes modificados

- `internal/tui/login_custom.go`
- `internal/tui/login_custom_test.go`
- `internal/providers/catalog_fetch_test.go`
- `tareas/en-proceso-01-login-modelos-contexto-opcional.md`
- `contexto/040-login-modelos-contexto-opcional.md`

# Problemas encontrados

- El flujo anterior ya podía consultar `/models`, pero después copiaba los IDs
  al input y exigía un segundo Enter. Eso hacía que dejar el campo vacío no
  funcionara como una selección automática real.
- La sintaxis de contexto manual (`modelo=1000000`) existía, pero la pantalla no
  la explicaba con claridad como una opción.
- El entorno de ejecución disponible tiene Go 1.23.2, mientras `go.mod` exige
  Go 1.24.0, y además no puede resolver `proxy.golang.org`; por eso no fue
  posible compilar el paquete TUI con sus dependencias externas.

# Soluciones implementadas

- La respuesta correcta de `/models` finaliza el login inmediatamente.
- La pantalla ahora indica `Modelos y contexto (opcional)` y documenta el
  formato manual `modelo, otro=1000000`.
- Se eliminó el estado `discovered`, que dejó de ser necesario.
- Se añadió una prueba de `FetchModels` con servidor HTTP local para validar
  ruta `/v1/models`, Bearer token, IDs y contexto resoluble.
- Se añadieron pruebas TUI para el flujo de Enter vacío y la ruta manual con
  contexto explícito.

# Pendientes

- Ejecutar localmente `go test ./...` con Go 1.24+ y acceso a las dependencias
  del módulo. La prueba aislada de `internal/providers` sí pasó usando una copia
  temporal compatible con Go 1.23 y `GOPROXY=off`.

# Próximos pasos

1. Probar `/login` contra un proveedor real compatible dejando Modelos vacío.
2. Confirmar que `/models` muestra los modelos recién descubiertos.
3. Ejecutar la suite completa localmente antes de marcar la tarea como
   `completado-`.
