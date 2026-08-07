package memory

import (
	"strings"

	"github.com/lilith/li/internal/moduleapi"
)

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.memory", Name: "Memory", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Estado y activación de memoria/instrucciones persistentes.",
		Commands: []moduleapi.Command{{
			Name: "memory", Usage: "[on|off|status]", Order: 800,
			Description: "Muestra instrucciones/memoria Claude-compatible y permite activar o desactivar auto memory.",
			Handler:     run,
		}},
	})
}

func run(host moduleapi.Host, args string) {
	ctl, ok := host.(moduleapi.MemoryController)
	if !ok {
		host.AddError("El host actual no expone memoria.")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "on", "enable", "activar":
		if err := ctl.SetAutoMemory(true); err != nil {
			host.AddError("No se pudo activar memoria: " + err.Error())
			return
		}
		host.AddSystem("Memoria automática activada.\n" + ctl.MemorySummary())
	case "off", "disable", "desactivar":
		if err := ctl.SetAutoMemory(false); err != nil {
			host.AddError("No se pudo desactivar memoria: " + err.Error())
			return
		}
		host.AddSystem("Memoria automática desactivada.")
	case "", "status", "show":
		host.AddSystem(ctl.MemorySummary())
	default:
		host.AddSystem("Uso: /memory [on|off|status]")
	}
}
