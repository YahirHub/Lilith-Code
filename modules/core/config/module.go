package config

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.config", Name: "Configuration", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Pantalla de configuración de Lilith.",
		Commands: []moduleapi.Command{{
			Name: "config", Aliases: []string{"settings"}, Order: 1800,
			Description: "Abre configuración de General, Búsqueda y Seguridad.",
			Handler: func(host moduleapi.Host, _ string) {
				opener, ok := host.(moduleapi.ScreenOpener)
				if !ok {
					host.AddError("El host actual no puede abrir configuración.")
					return
				}
				opener.OpenScreen(moduleapi.ScreenConfig)
			},
		}},
	})
}
