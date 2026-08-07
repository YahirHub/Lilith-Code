package rewind

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.rewind", Name: "Rewind", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Restauración segura de conversación y workspace.",
		Commands: []moduleapi.Command{{
			Name: "rewind", Order: 650,
			Description: "Restaura código, conversación o ambos al punto anterior a un mensaje del usuario.",
			Handler: func(host moduleapi.Host, _ string) {
				state, ok := host.(moduleapi.RewindState)
				if !ok {
					host.AddError("El host actual no expone la capacidad de rewind.")
					return
				}
				if state.RewindBusy() {
					host.AddError("/rewind sólo puede ejecutarse cuando el agente, los comandos directos y las tareas en background están inactivos. Cancela o espera a que terminen y vuelve a intentarlo.")
					return
				}
				opener, ok := host.(moduleapi.ScreenOpener)
				if !ok {
					host.AddError("El host actual no puede abrir la pantalla de rewind.")
					return
				}
				opener.OpenScreen(moduleapi.ScreenRewind)
			},
		}},
	})
}
