package fork

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.fork", Name: "Session Fork", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Forks independientes de conversación y workspace.",
		Commands: []moduleapi.Command{{
			Name: "fork", Usage: "[título opcional]", Order: 700,
			Description: "Crea una conversación y workspace independientes eligiendo una carpeta vacía en un navegador interactivo.",
			Handler: func(host moduleapi.Host, args string) {
				ctl, ok := host.(moduleapi.SessionForker)
				if !ok {
					host.AddError("El host actual no expone forks de sesión.")
					return
				}
				ctl.ForkSession(args)
			},
		}},
	})
}
