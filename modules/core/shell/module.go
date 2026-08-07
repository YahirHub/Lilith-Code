package shell

import (
	"strings"

	"github.com/lilith/li/internal/moduleapi"
)

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.shell", Name: "Direct Shell", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Entrada directa a la ejecución de shell desde el chat.",
		Commands: []moduleapi.Command{{
			Name: "bash", Aliases: []string{"!"}, Usage: "<comando>", Order: 2100,
			Description: "Ejecuta shell directamente con !comando; en Plan sólo admite inspección segura.",
			Handler: func(host moduleapi.Host, args string) {
				if strings.TrimSpace(args) == "" {
					host.AddSystem("Uso: !comando")
					return
				}
				submitter, ok := host.(moduleapi.Submitter)
				if !ok {
					host.AddError("El host actual no puede ejecutar una instrucción de shell.")
					return
				}
				submitter.Submit("!" + args)
			},
		}},
	})
}
