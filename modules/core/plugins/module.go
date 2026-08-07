package plugins

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.plugins", Name: "Plugins", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Plugins locales Claude-compatible.",
		Commands: []moduleapi.Command{
			{Name: "plugins", Order: 1200, Description: "Lista plugins locales Claude detectados y sus componentes namespaced.", Handler: func(host moduleapi.Host, _ string) {
				if ctl, ok := host.(moduleapi.PluginController); ok {
					ctl.RunPlugins()
				} else {
					host.AddError("El host actual no expone plugins.")
				}
			}},
			{Name: "reload-plugins", Order: 1300, Description: "Fuerza un rescan de plugins locales Claude para el siguiente turno.", Handler: func(host moduleapi.Host, _ string) {
				if ctl, ok := host.(moduleapi.PluginController); ok {
					ctl.ReloadPlugins()
				} else {
					host.AddError("El host actual no expone plugins.")
				}
			}},
		},
	})
}
