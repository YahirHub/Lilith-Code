# 104 — Pegado completo en proveedor personalizado

## Problema

En el alta de un proveedor personalizado, al pegar una URL base parecía que el
endpoint quedaba incompleto. El editor sí podía conservar más contenido, pero el
campo se renderizaba con el ancho predeterminado de `textinput` (20 columnas),
aunque la caja de ajustes fuese mucho más ancha. Como la vista siempre recortaba
desde el final, sólo se mostraba el inicio de la URL y el cursor podía quedar
fuera de la región visible.

Además, nombre, URL y API key compartían un límite único de 2,048 runes. Aunque
una URL normal no suele alcanzar ese tamaño, un endpoint generado o una
credencial extensa sí podía sufrir truncado real al pegarse.

## Solución

- El formulario custom calcula el ancho interior real de la caja y renderiza el
  `textinput` usando todas las columnas disponibles.
- El `textinput` de una sola línea incorpora una ventana horizontal que sigue al
  cursor. La vista selecciona únicamente el fragmento visible, pero nunca cambia
  ni corta el valor almacenado.
- El nombre conserva su límite anterior de 2,048 runes.
- URL base y API key admiten hasta 16,384 runes, igualando el orden de magnitud de
  otros editores de configuración extensos.
- El pegado continúa llegando como un único `KeyMsg` atómico desde tview/Tcell;
  no se introduce otro manejador de portapapeles ni se fragmenta el texto.

## Cobertura añadida

- Un endpoint pegado de más de 4,000 caracteres permanece byte/rune por rune en
  el campo del formulario.
- Una URL normal mayor que las antiguas 20 columnas aparece completa cuando el
  terminal tiene espacio suficiente.
- Un `textinput` estrecho muestra el final del valor y el cursor sin alterar el
  contenido original ni exceder su ancho visible.

## Validación realizada

- `gofmt` y `git diff --check` sin errores.
- Las pruebas de `internal/tui/uikit/textinput` compilaron y pasaron en un harness
  local aislado con Go 1.23 y un stub temporal de medición Unicode, usado sólo
  porque el entorno no dispone de red ni de los módulos oficiales en caché.
- La suite oficial de `internal/tui` no pudo ejecutarse aquí: el repositorio exige
  Go 1.24 y faltan `tview`, `tcell`, `uniseg`, `x/net`, `x/text` y gramáticas en
  caché. El cambio no modifica `go.mod` ni `go.sum`.

## Prueba manual requerida

1. Abrir `/providers` → **Agregar proveedor**.
2. Avanzar a **URL base** y pegar una URL más larga que el ancho del campo.
3. Confirmar que se ve el final junto al cursor y que `Home`/`End` permiten
   recorrer inicio y final sin perder caracteres.
4. Continuar, volver al paso anterior y comprobar que la URL sigue completa.
5. Guardar el proveedor y verificar en `/providers` y `providers.json` que la URL
   normalizada corresponde al endpoint introducido.
6. Repetir el pegado de una API key extensa y confirmar que se conserva completa
   aunque se muestre enmascarada.
