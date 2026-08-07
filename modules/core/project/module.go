package project

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.project", Name: "Project Initialization", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Inicialización y documentación persistente del proyecto.",
		Commands: []moduleapi.Command{{
			Name: "init", Order: 200,
			Description: "Analiza el proyecto y crea o mejora LILITH.md con instrucciones persistentes.",
			Handler: func(host moduleapi.Host, _ string) {
				runner, ok := host.(moduleapi.ProjectInitializer)
				if !ok {
					host.AddError("El host actual no expone inicialización de proyecto.")
					return
				}
				runner.InitializeProject()
			},
		}},
	})
}
