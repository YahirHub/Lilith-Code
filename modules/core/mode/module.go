package mode

import (
	"strings"

	"github.com/lilith/li/internal/moduleapi"
)

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.mode", Name: "Agent Modes", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Selección de los agentes Build y Plan.",
		Commands: []moduleapi.Command{
			{
				Name: "plan", Usage: "[show|status|exit|<instrucción>]", Order: 400,
				Description: "Activa Plan para explorar sin mutar; show muestra el plan listo y exit vuelve a Build.",
				Handler:     runPlan,
			},
			{
				Name: "build", Order: 500,
				Description: "Selecciona Build para el siguiente turno y restaura herramientas de implementación.",
				Handler: func(host moduleapi.Host, _ string) {
					ctl, ok := host.(moduleapi.AgentModeController)
					if !ok {
						host.AddError("El host actual no expone selección de agente.")
						return
					}
					ctl.SetAgentMode("build")
					ctl.SyncAgentModeUI()
				},
			},
		},
	})
}

func runPlan(host moduleapi.Host, args string) {
	ctl, ok := host.(moduleapi.AgentModeController)
	if !ok {
		host.AddError("El host actual no expone Plan.")
		return
	}
	arg := strings.TrimSpace(args)
	switch strings.ToLower(arg) {
	case "show":
		plan := strings.TrimSpace(ctl.LatestPlan())
		if plan == "" {
			host.AddSystem("No hay un plan completado en esta sesión.")
		} else {
			host.AddSystem("Plan actual:\n\n" + plan)
		}
	case "status":
		host.AddSystem("Agente seleccionado: " + strings.ToUpper(ctl.AgentMode()) + ". Tab recorre Build / Plan / Goal para el siguiente turno.")
	case "exit", "off", "build":
		ctl.SetAgentMode("build")
		ctl.SyncAgentModeUI()
	case "":
		ctl.SetAgentMode("plan")
		ctl.SyncAgentModeUI()
	default:
		ctl.SetAgentMode("plan")
		submitter, ok := host.(moduleapi.Submitter)
		if !ok {
			host.AddError("El host actual no puede enviar la instrucción a Plan.")
			return
		}
		submitter.Submit(arg)
	}
}
