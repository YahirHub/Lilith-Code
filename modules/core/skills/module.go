package skills

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.skills", Name: "Skills", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Invocación explícita de Agent Skills mediante slash routes.",
		Routes: []moduleapi.Route{{
			Prefix: "skills:", Aliases: []string{"skill:"}, Usage: "<nombre> [instrucciones extra]",
			Description: "Invoca una skill compatible por nombre.", Kind: "skill",
			Handler: func(host moduleapi.Host, target, args string) {
				if target == "" {
					host.AddError("Uso: /skills:<nombre> [instrucciones extra]")
					return
				}
				invoker, ok := host.(moduleapi.SkillInvoker)
				if !ok {
					host.AddError("El host actual no expone la capacidad de Agent Skills.")
					return
				}
				invoker.InvokeSkill(target, args)
			},
		}},
	})
}
