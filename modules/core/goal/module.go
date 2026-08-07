package goal

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.goal", Name: "Goal", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Objetivos persistentes para trabajo autónomo de larga duración.",
		Commands: []moduleapi.Command{{
			Name: "goal", Usage: "<objetivo>|status|pause|resume|complete|clear", Order: 300,
			Description: "Define o administra un objetivo persistente para trabajo autónomo de larga duración.",
			Handler: func(host moduleapi.Host, args string) {
				runner, ok := host.(moduleapi.GoalController)
				if !ok {
					host.AddError("El host actual no expone objetivos persistentes.")
					return
				}
				runner.RunGoal(args)
			},
		}},
	})
}
