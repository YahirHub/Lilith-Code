package compaction

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.compaction", Name: "Compaction", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Compactación del contexto conversacional.",
		Commands: []moduleapi.Command{{
			Name: "compact", Usage: "[instrucciones opcionales]", Order: 600,
			Description: "Resume el historial antiguo, conserva los turnos recientes y libera contexto sin borrar el transcript.",
			Handler: func(host moduleapi.Host, args string) {
				ctl, ok := host.(moduleapi.Compactor)
				if !ok {
					host.AddError("El host actual no expone compactación.")
					return
				}
				ctl.Compact(args)
			},
		}},
	})
}
