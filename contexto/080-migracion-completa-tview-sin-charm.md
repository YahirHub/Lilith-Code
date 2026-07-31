# 080 · Migración completa a tview sin dependencias Charmbracelet

## Fecha

2026-07-31

## Objetivo

Completar la fase iniciada en `079-migracion-runtime-interactivo-tview.md` y
retirar del proyecto todas las dependencias de Bubble Tea, Bubbles, Lip Gloss,
Glamour y `x/cellbuf`, sin perder el diseño ni el comportamiento ya estabilizado
del agente.

## Resultado

La migración de librería queda completa:

- `tview.Application` es el único propietario del ciclo físico del terminal.
- `tview.TextView` es la primitiva raíz que dibuja la interfaz completa.
- `tview.TranslateANSI` convierte el ANSI generado por Lilith a estilos nativos
  de tview/Tcell.
- Tcell entrega teclado, mouse, resize y eventos de pegado al runtime.
- No existe ningún import `github.com/charmbracelet/*` en el código.
- `go.mod` y `go.sum` ya no contienen módulos Charmbracelet.
- Los nombres heredados `tea.*` fueron sustituidos por `uikit.*` para evitar una
  falsa apariencia de compatibilidad con Bubble Tea.

## Arquitectura vigente

```text
Windows Terminal / PowerShell / CMD / terminal Unix
                         │
                         ▼
                tview.Application
       ciclo · pantalla · paste · mouse · redraw
                         │
                         ▼
              tviewModelSurface
                 tview.TextView
          TranslateANSI → estilos Tcell
                         │
                         ▼
            internal/tui/uikit
     mensajes · comandos · estado · widgets propios
                         │
                         ▼
 RootModel · ChatModel · ajustes · selectores · paneles
```

La interfaz mantiene una máquina de estado propia porque el agente necesita
coordinar streaming, herramientas, colas, sesiones y pantallas persistentes. Esa
máquina ya no pertenece a un framework externo: vive en `internal/tui/uikit` y
se ejecuta sobre el runtime de tview.

## Componentes internos sustitutos

### `uikit`

Define únicamente las abstracciones requeridas por Lilith:

- `Msg`, `Cmd` y `Model`;
- `Batch`, `Tick`, `Quit` y `WindowSizeMsg`;
- eventos de teclado y mouse;
- ejecución asíncrona y cola ordenada de mensajes.

### `uikit/textarea`

Editor multilínea propio con:

- pegado atómico;
- límite de 20,000 caracteres configurado por el chat;
- cursor y edición básica;
- saltos de línea normalizados;
- crecimiento visual independiente del contenido lógico;
- conservación de textos superiores a ocho líneas.

### `uikit/textinput`

Campo de una línea para formularios, login y búsquedas. Conserva foco, cursor,
límite de caracteres y modo contraseña.

### `uikit/viewport`

Viewport con offset vertical, navegación, porcentaje de scroll y autoscroll.
Mantiene todo el transcript fuera de la primitiva física de tview para no perder
el estado al navegar entre pantallas.

### `uikit/style` y `uikit/ansi`

Reemplazan el render de Lip Gloss y los helpers de Charm:

- colores RGB ANSI;
- bordes, padding, ancho y alto;
- joins horizontales y verticales;
- cálculo de ancho Unicode con `rivo/uniseg`;
- truncado, eliminación de escapes y wrap ANSI seguro sin dividir grafemas.

### Markdown

`internal/tui/markdown.go` ya no usa Glamour. Renderiza el subconjunto necesario
para el chat: encabezados, bloques de código, citas, listas, énfasis y código en
línea, respetando el ancho disponible.

## Entrada, pegado y espacio

- `Application.EnablePaste(true)` habilita bracketed paste.
- `PasteHandler` recibe el texto completo en una sola llamada.
- CRLF y CR se normalizan a LF antes de insertarse.
- El espacio conserva explícitamente `Runes: []rune{' '}`.
- El límite visual de ocho filas no limita las líneas almacenadas.
- La cola física es FIFO y no bloquea el event loop de tview.

## Ctrl+C y señales

`tview` normalmente usa Ctrl+C para cerrar. El runtime clona el evento en
`SetInputCapture`, evitando el cierre automático y entregándolo a Lilith para
mantener esta prioridad:

1. cancelar una solicitud en cola;
2. cancelar el turno activo;
3. cerrar la aplicación sólo cuando no haya trabajo pendiente.

SIGINT y SIGTERM externos siguen restaurando la terminal correctamente.

## Dependencias vigentes de TUI

```text
github.com/rivo/tview v0.42.0
github.com/gdamore/tcell/v2 v2.13.10
github.com/rivo/uniseg v0.4.7
```

No quedan Bubble Tea, Bubbles, Lip Gloss, Glamour, `x/ansi` ni `x/cellbuf`.

## Pruebas automáticas

Se conserva la suite existente y se añadió
`TestTViewMigrationHasNoCharmbraceletDependency`, que falla si:

- `go.mod` o `go.sum` vuelve a incluir `github.com/charmbracelet/*`;
- cualquier archivo Go vuelve a importar un módulo Charmbracelet.

También se añadieron regresiones para:

- Enter, Tab, Backspace, Esc y Ctrl+J con los códigos reales de Tcell;
- pegado CRLF sin saltos duplicados;
- cursor del textarea en su posición lógica;
- `MaxHeight` como límite exclusivamente visual;
- ancho Unicode, wrap y truncado ANSI sin perder estilos;
- Markdown con énfasis y código en línea a través de saltos de wrap;
- contenido literal como `[red]` sin activar etiquetas de `TextView`.

En el entorno de trabajo se ejecutaron la suite completa, la suite TUI con
`-race` y una compilación cruzada `windows/amd64` mediante una copia con Go 1.23
y stubs locales de API exclusivamente para dependencias externas. El
repositorio exige Go 1.24 y el entorno no puede descargar esa toolchain ni los
módulos. Todas esas comprobaciones pasaron. Las firmas usadas se contrastaron
con las APIs oficiales de tview v0.42.0 y Tcell v2.13.10; la validación final con
sus implementaciones oficiales debe ejecutarse con Go 1.24.

## Pruebas manuales requeridas

### Chat

1. Escribir texto con espacios, acentos y emojis.
2. Pegar una línea superior a 5,000 caracteres.
3. Pegar documentación con más de 24 líneas y líneas vacías.
4. Confirmar que el editor muestra hasta ocho filas pero envía todo el texto.
5. Pegar durante streaming y verificar que no se congele.

### Navegación

1. Abrir `/models`, `/provider`, `/config`, `/history`, `/help`, MCP y plugins.
2. Volver con Esc durante una tarea activa.
3. Probar Tab, Shift+Tab, flechas, Home/End, PageUp/PageDown y Ctrl+C.
4. Probar scroll manual y recuperación de autoscroll.

### Terminales

1. Windows Terminal con PowerShell.
2. Windows Terminal con CMD.
3. Consola clásica de Windows si aún se soporta en el equipo objetivo.
4. Linux con una terminal compatible con xterm-256color.

## Riesgos conocidos

- El render Markdown interno cubre el subconjunto usado por Lilith, no toda la
  especificación CommonMark de Glamour.
- Los widgets internos preservan el comportamiento actual; no intentan exponer
  toda la API genérica de los componentes retirados.
- El diseño se genera como ANSI y se traduce con `tview.TranslateANSI`; esto
  permite conservar paridad visual sin duplicar cada panel como una jerarquía de
  widgets stock de tview.

## Archivos principales

- `internal/tui/tview_runtime.go`
- `internal/tui/tcell_bridge.go`
- `internal/tui/markdown.go`
- `internal/tui/uikit/uikit.go`
- `internal/tui/uikit/ansi/ansi.go`
- `internal/tui/uikit/style/style.go`
- `internal/tui/uikit/textarea/textarea.go`
- `internal/tui/uikit/textinput/textinput.go`
- `internal/tui/uikit/viewport/viewport.go`
- `internal/tui/framework_migration_test.go`
- `go.mod`
- `go.sum`
