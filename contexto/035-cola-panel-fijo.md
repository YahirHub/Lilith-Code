# 035 · Cola de tareas en panel fijo (no en transcript)

## Contexto
Al enviar una solicitud mientras Lilith ya trabaja, el aviso `📥 En cola…`
se insertaba como mensaje de sistema dentro del transcript. Con el scroll
del propio turno activo, ese aviso subía y quedaba fuera de vista, así que
el usuario perdía visibilidad de lo que había en cola.

## Decisión
Renderizar la cola en un **panel fijo** justo encima de la caja de entrada
(entre transcript y textarea). No forma parte del historial: se re-renderiza
en cada `View()` a partir de `m.queue`, aparece cuando hay elementos y
desaparece cuando la cola queda vacía.

## Detalles de implementación (internal/tui/chat.go)
- Nuevo `queuePanelView(w)`: caja con borde redondeado, cabecera
  `📥 En cola · N pendiente(s) · …`, y hasta 5 items truncados a una línea
  (`truncateOneLine`) con el resto colapsado como `… y X más`.
- `bottomChromeHeight` y `View()` incluyen el panel al calcular el alto
  del viewport y al componer la vista.
- `submit()` en modo streaming: sólo hace `append` a `m.queue` y llama a
  `Resize` para que el viewport ceda alto al panel. Ya **no** empuja
  mensajes al transcript.
- `drainQueue()`: al desencolar llama a `Resize` para que la caja vuelva a
  su tamaño original; ya no empuja `▶ Ejecutando siguiente…` al transcript
  (el propio mensaje del usuario se renderiza como turno normal).
- `Ctrl+C` (con y sin tarea activa): tras vaciar la cola, hace `Resize`
  para que el panel desaparezca sin dejar hueco.

## Resultado
- La cola siempre está visible mientras hay pendientes.
- Nada se pierde con el scroll del transcript.
- Ctrl+C sigue funcionando en todos los modos (chat, onboarding, login,
  history) sin cambios.

## Commit sugerido
**summary:** `fix(tui): mostrar la cola de tareas en un panel fijo sobre la entrada`

**description:**
El aviso de "en cola" se añadía al transcript y se perdía con el scroll
del turno activo. Ahora se renderiza en un panel fijo encima de la caja
de entrada, se actualiza en cada tick, y se recalcula la altura del
viewport para que no tape mensajes. Ctrl+C limpia la cola y ajusta la
vista.
