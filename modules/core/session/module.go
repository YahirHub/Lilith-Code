package session

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.session", Name: "Session", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Ciclo de vida, historial y transferencia portable de conversaciones.",
		Commands: []moduleapi.Command{
			{Name: "clear", Aliases: []string{"new", "c", "n", "reset"}, Order: 1900, Description: "Empieza una conversación nueva y vuelve al agente Build.", Handler: func(host moduleapi.Host, _ string) {
				if ctl, ok := host.(moduleapi.SessionController); ok {
					ctl.ClearConversation()
				} else {
					host.AddError("El host actual no expone ciclo de vida de sesión.")
				}
			}},
			{Name: "history", Aliases: []string{"chats"}, Order: 2000, Description: "Abre el historial de conversaciones del proyecto.", Handler: openHistory},
			{Name: "export", Usage: "/export nombredechat.jsonl", Order: 2050, Description: "Exporta la conversación y su progreso a un JSONL portable.", Handler: func(host moduleapi.Host, args string) {
				transfer, ok := host.(moduleapi.SessionTransferController)
				if !ok {
					host.AddError("El host actual no puede exportar conversaciones.")
					return
				}
				message, err := transfer.ExportConversation(args)
				if err != nil {
					host.AddError(err.Error())
					return
				}
				host.AddSystem(message)
			}},
			{Name: "import", Usage: "/import nombredechat.jsonl", Order: 2100, Description: "Importa un JSONL y lo vincula al directorio actual como una sesión nueva.", Handler: func(host moduleapi.Host, args string) {
				transfer, ok := host.(moduleapi.SessionTransferController)
				if !ok {
					host.AddError("El host actual no puede importar conversaciones.")
					return
				}
				message, err := transfer.ImportConversation(args)
				if err != nil {
					host.AddError(err.Error())
					return
				}
				host.AddSystem(message)
			}},
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
