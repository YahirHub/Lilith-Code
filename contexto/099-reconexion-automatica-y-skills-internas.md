# Reconexión automática y retirada de skills internas

## Objetivos

1. Evitar que una caída temporal de Internet o del proveedor termine el turno
   con mensajes crudos como `dial tcp`, `network is unreachable` o EOF.
2. Retirar las dos skills internas específicas de Termux que enseñaban a
   instalar, compilar y publicar Lilith.

## Transporte de proveedor

`internal/providers/openai` separa ahora los fallos en dos grupos:

- errores HTTP recuperables de servicio, que conservan reintentos acotados;
- interrupciones de transporte, que mantienen vivo el request lógico hasta que
  la conexión regrese o el usuario pulse `Esc`.

Ante una interrupción se comprueba primero el `BaseURL` del proveedor. Si no
responde, se consultan endpoints públicos de conectividad para distinguir:

- equipo sin Internet;
- Internet disponible pero proveedor inaccesible;
- proveedor nuevamente alcanzable.

La espera usa backoff de 1 a 15 segundos. No bloquea Tcell/Tview ni la escritura
en el input. El usuario puede seguir encolando steering/follow-up o cancelar.

Si el stream se corta después de emitir contenido, el cliente envía una señal
de reset antes del reintento. La TUI elimina sólo los mensajes, reasoning y
paneles parciales creados por ese request; no altera el prompt, el historial
estable ni las herramientas ya completadas. También se consideran recuperables
los cierres abruptos del transporte, EOF inesperado y timeouts de inactividad.
Un cierre HTTP limpio sigue siendo compatible con proveedores SSE que no envían
un marcador final propio. La misma señal de reset se propaga a subagentes y a la
compactación automática para que tampoco acumulen contenido duplicado.

## Skills internas

Se eliminaron del árbol embebido:

- la skill de desarrollo Termux;
- la skill de compilación/publicación Termux;
- sus referencias y menciones en agentes/documentación.

Lilith conserva la infraestructura general de skills y sigue leyendo skills de
usuario/proyecto. Los agentes `termux-specialist` y `termux-auditor` permanecen,
pero se limitan a portabilidad de runtime y prohíben actuar como guía de
instalación, compilación o releases de Lilith.

## Validación

```bash
gofmt -w internal/providers/openai/client.go \
  internal/providers/openai/retry.go \
  internal/providers/openai/client_transport_test.go \
  internal/subagents/events.go internal/subagents/runtime.go \
  internal/subagents/runtime_test.go internal/tui/agent_panel.go \
  internal/tui/chat.go internal/tui/chat_streaming_input_test.go \
  internal/tui/compaction.go internal/skills/bundled_test.go

go test ./internal/providers/openai ./internal/skills ./internal/agents
go test ./internal/subagents ./internal/tui

git grep -n -E 'termux-development|termux-release'
git diff --check
```

El workflow con Go 1.24 debe ejecutar finalmente `go test ./...`.
