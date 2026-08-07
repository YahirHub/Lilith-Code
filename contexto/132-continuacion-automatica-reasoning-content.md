# 132. Continuación automática tras `reasoning_content` persistente

## Problema observado

El cliente OpenAI-compatible ya trataba el HTTP 400 específico de
`reasoning_content` como transitorio y reintentaba silenciosamente hasta tres
veces. En algunos gateways saturados los tres intentos podían caer en el mismo
upstream incompatible. En ese caso el error final llegaba a la TUI y el turno
terminaba, aunque escribir manualmente `continue` inmediatamente después hacía
que el proveedor aceptara la siguiente solicitud y Lilith continuara con
normalidad.

El caso real se observó justo después de una tool call (`str_replace`): la tool
había terminado correctamente, pero la continuación del modelo recibió:

```text
HTTP 400: ... The `reasoning_content` in the thinking mode must be passed back to the API.
```

## Corrección

Se mantienen intactos los reintentos HTTP existentes. Si después de agotarlos
el mismo error llega a la TUI, Lilith aplica un segundo nivel de recuperación
sólo cuando el historial está en una frontera segura post-tool:

1. No muestra `Error del proveedor` en el primer fallo recuperable.
2. Mantiene el mismo turno del usuario activo.
3. Si el usuario ya escribió steering mientras esperaba, usa esa instrucción
   como la nueva frontera de usuario.
4. En caso contrario agrega únicamente al historial de protocolo una
   continuación interna equivalente a `continue`; no aparece en el transcript.
5. Lanza inmediatamente un nuevo request contra el mismo proveedor/modelo.
6. La recuperación está limitada a una vez por request. Si también falla el
   request recuperado, el error final sí se muestra para impedir ciclos
   infinitos.
7. Cuando un request termina correctamente, el permiso de recuperación se
   reinicia para que otra tool posterior del mismo turno pueda recuperarse de
   forma independiente.

La continuación interna sólo se habilita si no existe respuesta parcial,
reasoning parcial ni una tool call incompleta y si el último mensaje real del
historial es `role=tool`. Otros HTTP 400 permanecen terminales.

## API interna

`internal/providers/openai` expone
`IsReasoningContentCarryForwardError(error) bool` para que la TUI reutilice la
misma clasificación estricta que el cliente de transporte, sin duplicar strings
o relajar accidentalmente qué errores son recuperables.

## Pruebas

- La suite completa de `internal/providers/openai` pasa en una copia temporal
  compatible con el Go disponible en el entorno de entrega.
- Se añadieron regresiones TUI para comprobar que:
  - el primer error post-tool inicia otro request sin crear `MsgError`;
  - la continuación sintética no aparece como `MsgUser` visible;
  - el request recuperado recibe un ID nuevo manteniendo el mismo turno;
  - un segundo fallo consecutivo no crea un loop y sí termina mostrando el
    error final.
- Los 293 archivos Go parsean correctamente y `git diff --check` pasa.

La suite TUI no pudo compilarse en el entorno de entrega porque éste sólo tiene
Go 1.23.2 y no dispone de red para obtener el toolchain/dependencias requeridas
por el proyecto (`go 1.25.12`). Los tests añadidos quedan listos para `test.cmd`
en Windows.

## Archivos

- `internal/providers/openai/client.go`
- `internal/providers/openai/client_transport_test.go`
- `internal/tui/chat.go`
- `internal/tui/chat_streaming_input_test.go`
- `contexto/132-continuacion-automatica-reasoning-content.md`
