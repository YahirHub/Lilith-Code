---
name: Fix — el CommandPanel nunca se mostraba entre turnos
description: Los mapas cmdPanels/cmdByCall no se limpiaban al iniciar un nuevo turno, así que las llamadas nuevas a run_terminal_command reutilizaban silenciosamente el panel viejo (Done=true) del turno anterior y ningún panel visible aparecía; el shimmer "trabajando…" era lo único que se veía. Además el indicador "trabajando" se mantenía encendido durante el streaming del comando porque panels() sólo contaba paneles de archivo.
type: fix
---

## Problema

El usuario reportaba (screenshot 133) que al ejecutar comandos sólo aparecía
"trabajando…", desaparecía y volvía a aparecer, pero el panel bash-style con
el comando y su salida NUNCA se dibujaba.

## Causa

1. `runTurn()` limpiaba `livePanels`/`thinkingActive` pero NO
   `cmdPanels` ni `cmdByCall`. Como los índices del transporte (Codex u
   OpenAI) reinician desde 0 en cada turno, la nueva llamada `Index=0`
   encontraba el panel viejo (`Done=true`) en `cmdPanels[0]` y sólo lo
   actualizaba: no se creaba ningún mensaje nuevo en el transcript, así
   que visualmente no había panel.
2. El check `len(m.panels()) > 0` (línea 712) para apagar el shimmer
   sólo contaba `FilePanel`s. Con un CommandPanel activo, seguía en
   estado "thinking" durante el partial-stream del comando.
3. `applyToolCalls` indexaba estrictamente por `c.Index` sin verificar
   el `CallID`; un mismo Index con un CallID distinto reutilizaba el
   panel equivocado.

## Cambios

* `internal/tui/chat.go` (`runTurn`): también resetea `panelByCall`,
  `cmdPanels`, `cmdByCall` al arrancar un turno nuevo.
* `internal/tui/chat.go` (`applyToolCalls`): busca primero por
  `CallID` y sólo reutiliza el panel al mismo Index si el CallID
  coincide (o si aún no tiene uno) y no está Done. Se aplica tanto a
  FilePanel como a CommandPanel.
* `internal/tui/chat.go` (streamPump handler): el shimmer se apaga
  también cuando ya hay un `CommandPanel` en vivo, no sólo un
  `FilePanel`.

## Commit sugerido

fix(tui): mostrar el CommandPanel en cada turno y no reutilizar el del turno anterior
