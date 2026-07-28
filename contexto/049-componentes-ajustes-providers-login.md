# Fecha
2026-07-28

# Objetivo
Crear un estándar reutilizable para pantallas de configuración de Lilith y reemplazar el comportamiento poco útil de `/providers` por un administrador interactivo. Aplicar el mismo sistema visual y de interacción a `/login` y `/config` sin introducir un framework adicional ni dependencias nuevas.

# Decisiones tomadas
- Mantener los componentes dentro de `internal/tui` en un solo archivo (`settings_components.go`) en lugar de crear una jerarquía de widgets/paquetes prematura.
- Usar hitboxes de celdas calculados durante el render para clic izquierdo. Bubble Tea ya entrega coordenadas de mouse y el programa ya inicia con mouse habilitado.
- Separar el render del estado de negocio: los componentes devuelven texto + hitboxes; cada pantalla conserva la responsabilidad de persistir o cambiar valores.
- Crear primitives para cards, botones, grupos de botones responsivos, switches, slider, stepper numérico, shell de input y textarea autoajustable.
- No crear una preferencia numérica ficticia para demostrar slider/stepper. Los controles existen y tienen pruebas, pero sólo se conectarán cuando haya un ajuste numérico real.
- El textarea autoajustable usa `SetHeight` para el viewport y mantiene `MaxHeight = 0`. En Bubbles v0.20.0 `MaxHeight` también limita las líneas insertables, por lo que usarlo como simple límite visual podía truncar pegados multilínea.
- `/providers` administra selección, activación, modelos, alta, eliminación y streaming; las credenciales continúan siendo responsabilidad de `/login`.
- Los proveedores bundled no permiten borrar ni cambiar su transporte desde esta pantalla.
- Cachear el catálogo remoto OpenCode Free una vez por proceso para que acciones locales en `/providers` no vuelvan a pagar el timeout de red.
- Mantener `/provider` como alias de `/providers`.

# Arquitectura actual
## Kit de ajustes
`settings_components.go` contiene controles sin estado de dominio:
- `settingsCard`: tarjeta seleccionable, badges, estado activo y wrapping de metadatos largos.
- `settingsButtonRow` / `settingsButtonGroup`: botones clicables que saltan de fila cuando no caben.
- `settingsSwitch`: switch ON/OFF clicable y navegable por teclado desde la pantalla consumidora.
- `settingsSlider`: track clicable con snapping a `Step`.
- `settingsStepper`: botones `−/+` con hitboxes separados.
- `settingsInput`: borde/estado de foco común para editores.
- `adaptiveTextArea`: crece de `minHeight` a `maxHeight` según líneas duras o wrapping, sin truncar el valor.
- `settingsCanvas`: compone bloques y traslada hitboxes a coordenadas reales después de centrar la pantalla.

## `/providers`
- `/providers` y `/provider` cambian a `ProviderScreen`.
- Cards de proveedores navegables con ↑/↓ o clic.
- Botones: Activar, Modelos, Agregar proveedor, Eliminar (custom), Volver.
- Switch `Streaming` persistente para providers personalizados (`UseNonStreaming`).
- Eliminación con segunda confirmación y limpieza de API key.
- Catálogo visible calculado por altura real de cards para mantener seleccionado el provider aun cuando una URL larga envuelva varias filas.

## `/login`
- Onboarding con cards clicables para ChatGPT/Codex, endpoint personalizado y OpenCode Free.
- Flujo Codex reutiliza cards/botones para copiar URL/código, reintentar y cancelar.
- Flujo custom mantiene cuatro pasos y usa el input estándar.
- Campo de modelos/contexto es multilinea autoajustable; Enter vacío sigue consultando `{baseURL}/models` y Alt+Enter inserta una línea explícita.

## `/config`
- El switch de Skills usa el mismo componente de ajustes y admite teclado/clic.
- Rutas y skills detectadas se muestran con cards del mismo sistema.

# Librerías usadas
No se agregaron dependencias. Se reutilizan las versiones ya fijadas:
- Bubble Tea v1.2.4.
- Bubbles v0.20.0.
- Lip Gloss existente en el proyecto.

# Archivos importantes modificados
- `internal/tui/settings_components.go`
- `internal/tui/settings_components_test.go`
- `internal/tui/provider_screen.go`
- `internal/tui/provider_screen_test.go`
- `internal/tui/commands.go`
- `internal/tui/onboarding.go`
- `internal/tui/login_custom.go`
- `internal/tui/login_custom_test.go`
- `internal/tui/login_codex.go`
- `internal/tui/config_screen.go`
- `internal/providers/upsert.go`
- `internal/providers/provider_management_test.go`
- `internal/providers/bundled.go`

# Problemas encontrados
- `/providers` sólo aportaba salida textual y no permitía administrar realmente el catálogo.
- Las pantallas de configuración tenían layouts y controles independientes, por lo que cada nueva opción requería volver a implementar interacción y estilo.
- Los botones podían desbordar horizontalmente si se agregaban varias acciones en terminales estrechas.
- Cards con URL/path largo podían romper el ancho esperado y los hitboxes.
- Fijar `textarea.MaxHeight` al máximo visual no era válido: Bubbles v0.20.0 usa ese valor también al insertar nuevas líneas, pudiendo truncar contenido pegado.
- Recargar providers podía volver a consultar OpenCode Free y hacer que una interacción local pareciera congelada por hasta el timeout de red.

# Soluciones implementadas
- Administrador `/providers` real con mouse/teclado y persistencia segura.
- Hit-testing reutilizable basado en el layout renderizado.
- Botones que envuelven por filas y cards/metadatos que se adaptan al ancho.
- Catálogo `/providers` adaptado a la altura real disponible.
- Textarea de modelos que cambia únicamente su altura visible y preserva todo el contenido.
- Cache de modelos bundled por proceso.
- Helpers persistentes `SetUseNonStreaming` y `Delete` para providers personalizados.
- Pruebas de persistencia, hitboxes, slider, stepper, wrapping y crecimiento del textarea.

# Pendientes
- Ejecutar `go test ./...` y `go vet ./...` en Windows con Go 1.24+.
- Validar manualmente clics en `/providers`, `/login` y `/config` dentro de Windows Terminal.
- Confirmar que alta/eliminación/streaming persisten después de reiniciar Lilith.

# Próximos pasos
Si la validación local es correcta, marcar la tarea 08 como completada. El slider y stepper quedan disponibles para futuras preferencias numéricas reales; no agregar opciones artificiales sólo para utilizarlos.
