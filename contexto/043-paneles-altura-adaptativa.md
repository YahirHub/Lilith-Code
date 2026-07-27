# Fecha
2026-07-27

# Objetivo
Reducir el espacio vacío de los paneles del transcript para que su altura crezca con el contenido y conserve como techo la altura máxima de vista previa que ya tenía cada tipo de panel.

# Decisiones tomadas
- No se fija una altura mínima artificial en los paneles.
- Se eliminan las líneas vacías usadas para rellenar `ThinkingPanel` y `FilePanel`.
- Se conservan los límites históricos: 6 líneas de razonamiento, 12 líneas de archivo/diff y 10 líneas de salida de comando.
- Cuando el contenido supera el máximo se reserva una fila del máximo para el aviso de líneas ocultas y el resto muestra las líneas más recientes.
- El modo expandido continúa mostrando todo el contenido sin truncarlo.
- `CommandPanel` ya crecía con contenido corto; se unifica su cálculo de ventana con los demás paneles para mantener el mismo criterio y corregir el conteo de líneas ocultas.
- No se usa `Height(...)` de Lip Gloss porque establece una altura mínima y produciría precisamente el espacio vacío que se quiere eliminar. El límite se controla sobre las líneas renderizadas antes de construir el box.

# Arquitectura actual
- `ThinkingPanel` renderiza cabecera + entre 0 y 6 filas de razonamiento en su vista visible.
- `FilePanel` renderiza cabecera + entre 0 y 12 filas de contenido/diff en preview.
- `CommandPanel` renderiza cabecera/tiempo + entre 0 y 10 filas de stdout/stderr en preview.
- `cappedTailPreview` concentra únicamente la regla compartida de limitar una vista conservando las líneas más recientes y dejando espacio para el aviso de contenido oculto.
- Los paneles siguen formando parte del transcript persistente; este cambio es exclusivamente de presentación.

# Librerías usadas
No se agregaron dependencias. Se reutiliza Lip Gloss ya presente en el proyecto y la librería estándar.

# Archivos importantes modificados
- `internal/tui/thinking_panel.go`
- `internal/tui/filepanel.go`
- `internal/tui/cmdpanel.go`
- `internal/tui/panel_preview.go`
- `internal/tui/filepanel_test.go`
- `internal/tui/panel_height_test.go`
- `internal/tui/chat.go`
- `tareas/en-proceso-04-paneles-altura-adaptativa.md`

# Problemas encontrados
- `ThinkingPanel` rellenaba con espacios hasta 6 filas incluso cuando el razonamiento tenía una o dos líneas.
- `FilePanel` rellenaba con espacios hasta 12 filas durante toda la vista previa, generando cajas visualmente muy altas para cambios pequeños.
- El algoritmo de truncado reemplazaba una línea real por el aviso de líneas ocultas después de calcular el recorte, por lo que el número mostrado como oculto podía quedar una línea por debajo del valor real.
- `CommandPanel` no añadía relleno, pero usaba la misma forma incorrecta de sustituir la primera línea al mostrar el aviso de salida anterior.

# Soluciones implementadas
- Altura adaptativa para reasoning y archivos: sin filas de relleno.
- Límite máximo idéntico al anterior para evitar que los paneles largos ocupen más transcript que antes.
- Helper pequeño `cappedTailPreview` para aplicar la misma regla a reasoning, archivos y comandos.
- Conteo correcto de líneas ocultas reservando primero una fila para el aviso.
- Pruebas de regresión para contenido corto, contenido por encima del máximo y modo expandido.

# Pendientes
- Ejecutar `go test ./...` y `go vet ./...` en un entorno con Go 1.24 y dependencias Charm disponibles. El sandbox actual no puede descargar el toolchain ni los módulos requeridos.
- Validar visualmente en Windows Terminal que los paneles crezcan fila a fila durante streaming sin introducir saltos molestos en el scroll.

# Próximos pasos
1. Compilar y ejecutar la suite completa en el equipo del usuario.
2. Probar reasoning de 1, 3, 6 y más de 6 líneas.
3. Probar `write_file`/`str_replace` con cambios pequeños y grandes.
4. Probar comandos con y sin salida, y con más de 10 líneas.
