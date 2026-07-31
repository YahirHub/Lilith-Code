# 081 · Corrección del viewport y navegación superior de `/config`

## Fecha

2026-07-31

## Problema

Después de completar la migración a `tview`, `/config` seguía generando toda la
pantalla como una única cadena ANSI. La raíz física usa un `tview.TextView` sin
scroll propio porque el estado y la navegación pertenecen a Lilith.

Cuando el contenido de General o Búsqueda superaba el alto de la terminal, el
`TextView` podía terminar mostrando la parte inferior. El foco lógico sí se
movía con `↑/↓`, pero `/config` no tenía un offset vertical que hiciera visible
el control seleccionado. Por eso era posible volver lógicamente a las secciones
superiores sin que el encabezado ni `General / Búsqueda / Seguridad` reaparecieran.

En Seguridad existía además una inconsistencia: al pulsar `↓` desde la barra de
secciones, el foco saltaba directamente a `Volver al chat` en vez de entrar por
`Proyecto confiable`.

## Solución

`ConfigScreen` conserva ahora el layout completo y aplica después una ventana
vertical limitada por `AppContext.Height`.

La nueva lógica:

- mantiene todo el contenido y todos los controles en el layout completo;
- guarda únicamente un `viewportOffset` como estado visual;
- desplaza la ventana cuando el control enfocado queda arriba o abajo del área
  física visible;
- traduce las coordenadas de mouse al recortar los hitboxes visibles;
- fuerza `viewportOffset = 0` cuando el foco regresa a la navegación superior;
- sigue al proveedor seleccionado en la lista de Búsqueda;
- sigue los controles de detalle, API key y orden de respaldos;
- conserva el comportamiento de `←/→` exclusivo de la barra superior;
- entra correctamente a `Proyecto confiable` al pulsar `↓` en Seguridad.

No se habilitó el scroll genérico de `tview.TextView`, porque el chat y otras
pantallas ya poseen sus propios viewports. El desplazamiento permanece dentro
de `/config`, asociado a su foco y sin interferir con el runtime global.

## Archivos modificados

- `internal/tui/config_screen.go`
- `internal/tui/config_screen_test.go`
- `contexto/081-fix-viewport-config-tview.md`

## Pruebas de regresión

Se añadieron pruebas para terminales de poca altura que comprueban:

1. El encabezado y las tres secciones se muestran al abrir `/config`.
2. Al bajar hasta `Volver al chat`, el viewport acompaña al foco.
3. Al subir nuevamente hasta la barra superior, el offset vuelve a cero y el
   encabezado reaparece.
4. Cada proveedor de Búsqueda permanece visible mientras se recorre la lista.
5. Desde el primer proveedor, una pulsación adicional de `↑` regresa de verdad
   a la barra superior visible.
6. Seguridad entra primero en `Proyecto confiable`.

## Validación realizada

En una copia de compilación compatible con la toolchain disponible y stubs
locales de las mismas APIs externas se ejecutaron correctamente:

```bash
go test ./internal/tui -run TestConfig -count=1
go test ./...
go test -race ./internal/tui
go vet ./...
go build ./cmd/li
GOOS=windows GOARCH=amd64 go build ./cmd/li
```

El repositorio mantiene `go 1.24.0`; la comprobación final en el equipo objetivo
debe hacerse con Go 1.24 real y las dependencias oficiales.

## Prueba manual requerida

1. Reducir la altura de Windows Terminal.
2. Abrir `/config` y confirmar que se vea el encabezado y la barra de secciones.
3. En General, bajar hasta `Volver al chat` y subir nuevamente hasta la barra.
4. En Búsqueda, recorrer los siete motores hasta el último y volver al primero.
5. Desde Tavily, pulsar `↑` y confirmar que reaparezcan el encabezado y las
   pestañas superiores.
6. Entrar a Seguridad y pulsar `↓`; debe enfocarse `Proyecto confiable`.
