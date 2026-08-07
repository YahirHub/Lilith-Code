package providers

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.providers", Name: "Providers", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Login, proveedores y selección de modelos.",
		Commands: []moduleapi.Command{
			{Name: "login", Aliases: []string{"signin"}, Order: 1500, Description: "Conecta un nuevo proveedor.", Handler: open(moduleapi.ScreenOnboarding)},
			{Name: "providers", Aliases: []string{"provider"}, Order: 1600, Description: "Administra proveedores configurados.", Handler: open(moduleapi.ScreenProviders)},
			{Name: "models", Aliases: []string{"model"}, Order: 1700, Description: "Elige el modelo activo.", Handler: open(moduleapi.ScreenModels)},
		},
	})
}

func open(screen moduleapi.Screen) moduleapi.CommandHandler {
	return func(host moduleapi.Host, _ string) {
		opener, ok := host.(moduleapi.ScreenOpener)
		if !ok {
			host.AddError("El host actual no puede abrir esta pantalla.")
			return
		}
		opener.OpenScreen(screen)
	}
}
