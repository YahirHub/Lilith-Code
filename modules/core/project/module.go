package project

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.project", Name: "Project Initialization", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Inicialización y documentación persistente del proyecto.",
		Commands: []moduleapi.Command{{
			Name: "init", Usage: "[instrucciones adicionales]", Order: 200,
			Description: "Analiza el proyecto y crea o mejora LILITH.md; acepta instrucciones adicionales para esta ejecución.",
			Handler: func(host moduleapi.Host, args string) {
				if runner, ok := host.(moduleapi.ProjectInitializerWithInstructions); ok {
					runner.InitializeProjectWithInstructions(args)
					return
				}
				runner, ok := host.(moduleapi.ProjectInitializer)
				if !ok {
					host.AddError("El host actual no expone inicialización de proyecto.")
					return
				}
				if args != "" {
					host.AddError("El host actual sólo admite /init sin instrucciones adicionales.")
					return
				}
				runner.InitializeProject()
			},
		}},
	})
}
