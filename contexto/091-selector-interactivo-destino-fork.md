# 091 — Selector interactivo de destino para `/fork`

## Problema anterior

`/fork [título opcional]` elegía automáticamente una ruta privada bajo el directorio de configuración de Lilith. El fork quedaba aislado, pero el usuario no podía decidir en qué disco, carpeta de trabajo o ubicación compartida debía crearse.

Usar un diálogo gráfico nativo no era una opción adecuada: Lilith también se ejecuta en servidores Linux por SSH, donde no existe escritorio ni selector de archivos del sistema.

## Nuevo flujo

Al ejecutar `/fork`, Lilith valida primero que exista una conversación y que no haya un turno, comando directo o subagente background activo. Después abre la pantalla `Destino del fork` y pospone la captura/materialización hasta que el usuario confirme una carpeta.

El navegador inicia en el directorio padre de la raíz del workspace fuente para facilitar la creación de una copia hermana. En Git usa la raíz del repositorio aunque Lilith se haya abierto desde un subproyecto; fuera de Git usa el directorio del proyecto activo. La lista ofrece:

1. usar la carpeta actual;
2. crear una carpeta nueva;
3. volver al directorio padre;
4. abrir las subcarpetas visibles;
5. en la raíz de Windows, cambiar entre unidades disponibles.

Al crear una carpeta, el navegador entra inmediatamente en ella. Como está vacía, puede seleccionarse directamente como destino final.

## Controles

La pantalla conserva funcionalidad completa sin mouse:

- `↑`/`↓`, `J`/`K` o `Ctrl+N`/`Ctrl+P`: mover la selección;
- `Enter`, `→` o `L`: abrir la carpeta o ejecutar la acción seleccionada;
- `Backspace`, `←`, `Alt+←` o `H`: volver al directorio padre;
- `N`: crear una carpeta;
- `S`: usar la carpeta actual;
- `Esc` o `Q`: cancelar y regresar al chat.

Cuando el terminal local o el cliente SSH transmite eventos de mouse, también funcionan:

- clic sobre carpetas y acciones;
- rueda para mover la selección;
- clic en el campo y en los botones del formulario de creación.

No se depende de una interfaz gráfica externa, por lo que la misma pantalla funciona en Windows Terminal, terminales Linux y sesiones SSH.

## Validaciones de seguridad

El destino final debe:

- existir y ser un directorio;
- estar completamente vacío;
- quedar fuera del proyecto/workspace original;
- usar, para carpetas nuevas, un nombre portable que no contenga separadores, caracteres inválidos ni nombres reservados de Windows.

La validación se aplica tanto en la TUI como en `internal/rewind`. El backend acepta ahora una carpeta vacía creada previamente por el selector, pero continúa rechazando rutas no vacías o anidadas dentro del workspace fuente. La comprobación del backend resuelve enlaces simbólicos existentes para evitar eludir la separación del fork mediante symlinks.

Si el backend fallback falla después de empezar a restaurar archivos, una carpeta seleccionada preexistente se limpia sin eliminarla; una ruta creada por el backend se elimina por completo.

## Archivos principales

- `internal/tui/fork_destination.go`: navegador, formulario, atajos y hitboxes de mouse.
- `internal/tui/rewind_state.go`: separa la apertura del selector de la creación efectiva del fork.
- `internal/rewind/store.go`: admite directorios vacíos existentes y valida el aislamiento del destino.
- `internal/tui/fork_destination_test.go`: navegación, regreso, creación, mouse y selección.
- `internal/rewind/rewind_test.go`: destinos vacíos, no vacíos, anidados y worktree sobre carpeta elegida.

## Prueba manual recomendada

1. Abrir un proyecto con al menos un mensaje en la conversación.
2. Ejecutar `/fork alternativa`.
3. Navegar sólo con teclado, volver con `Backspace` y crear una carpeta con `N`.
4. Confirmar `Usar esta carpeta vacía` y comprobar que Lilith cambia al fork.
5. Modificar un archivo y verificar que el proyecto original permanece intacto.
6. Repetir usando clic y rueda en un terminal con mouse habilitado.
7. Repetir por SSH usando únicamente teclado.
8. Intentar elegir una carpeta no vacía y otra dentro del proyecto original; ambas deben rechazarse.
