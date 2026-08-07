package help

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.help", Name: "Help", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Referencia interactiva de comandos y atajos.",
		Commands: []moduleapi.Command{{
			Name: "help", Aliases: []string{"h", "?"}, Order: 100,
			Description: "Abre la referencia completa de comandos y atajos de teclado.",
			Handler: func(host moduleapi.Host, _ string) {
				opener, ok := host.(moduleapi.ScreenOpener)
				if !ok {
					host.AddError("El host actual no puede abrir la ayuda.")
					return
				}
				opener.OpenScreen(moduleapi.ScreenHelp)
			},
		}},
	})
}
