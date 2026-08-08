package session

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.session", Name: "Session", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Ciclo de vida e historial de conversaciones.",
		Commands: []moduleapi.Command{
			{Name: "clear", Aliases: []string{"new", "c", "n", "reset"}, Order: 1900, Description: "Empieza una conversación nueva y vuelve al agente Build.", Handler: func(host moduleapi.Host, _ string) {
				if ctl, ok := host.(moduleapi.SessionController); ok {
					ctl.ClearConversation()
				} else {
					host.AddError("El host actual no expone ciclo de vida de sesión.")
				}
			}},
			{Name: "history", Aliases: []string{"chats"}, Order: 2000, Description: "Abre el historial de conversaciones del proyecto.", Handler: openHistory},
			{Name: "exit", Order: 2200, Description: "Cierra Lilith. Es la única salida explícita del proceso.", Handler: func(host moduleapi.Host, _ string) {
				if ctl, ok := host.(moduleapi.SessionController); ok {
					ctl.ExitApplication()
				} else {
					host.AddError("El host actual no puede cerrar la aplicación.")
				}
			}},
		},
	})
}

func openHistory(host moduleapi.Host, _ string) {
	opener, ok := host.(moduleapi.ScreenOpener)
	if !ok {
		host.AddError("El host actual no puede abrir el historial.")
		return
	}
	opener.OpenScreen(moduleapi.ScreenHistory)
}
