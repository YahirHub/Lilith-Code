# Preguntas de Plan integradas en el footer

## Motivo

La primera UI interactiva de `plan_question` sustituyó el chat por una pantalla de ajustes con cajas y botones grandes. Aunque era funcional, consumía demasiado espacio y no seguía el patrón de OpenCode, donde las preguntas viven en la zona inferior de la sesión.

## Comportamiento

- `plan_question` termina el turno de Plan y abre un dock compacto dentro del propio chat.
- Se muestra una sola pregunta pendiente a la vez.
- Las opciones son filas planas, sin cards ni pantalla secundaria.
- `Up/Down`, `j/k`, `Tab/Shift+Tab`, `1-9` y `Enter` permiten responder.
- `Otra respuesta` usa un input de una sola línea dentro del mismo dock.
- `Esc` cierra solamente el dock. La solicitud y las respuestas parciales siguen persistidas.
- Cuando el dock está cerrado aparece `?` como launcher. Se puede hacer clic o pulsar `?` para continuar.
- Las preguntas sobreviven a cambios Plan/Build y a reanudación de sesión mientras sigan siendo la última frontera de decisión.
- Un nuevo turno real del usuario sustituye la solicitud pendiente.
- Si el usuario seleccionó Build mientras un turno Plan esperaba respuestas, las respuestas continúan ese turno en Plan y Build se conserva para el siguiente turno normal.

## Pantallas pequeñas

El dock limita deliberadamente su altura:

- pregunta de hasta dos filas; una fila en terminales bajas;
- hasta cuatro opciones visibles;
- tres opciones si la terminal tiene 18 filas o menos;
- dos opciones si tiene 12 filas o menos;
- el resto se recorre con navegación vertical.

Mientras el dock está abierto reemplaza temporalmente el editor normal y los widgets inferiores no esenciales, dejando el máximo alto posible al transcript.

## Estado persistente

`plan.State.QuestionAnswers` guarda las respuestas parciales por ID. La presentación (`open`, selección, edición) no se persiste: al reanudar una sesión queda el launcher `?`, no una pantalla forzada.

## Mouse

El chat normalmente libera el mouse para selección nativa de texto. Sólo mientras haya preguntas Plan pendientes se habilita mouse reporting para permitir clic sobre opciones o el launcher `?`. Al responder o sustituir la solicitud se vuelve a desactivar.
