package mcp

import "github.com/lilith/li/internal/moduleapi"

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.mcp", Name: "MCP", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Inspección y reconexión de servidores MCP.",
		Commands: []moduleapi.Command{{
			Name: "mcp", Usage: "[reload]", Order: 900,
			Description: "Muestra servidores/herramientas MCP conectados o fuerza una reconexión.",
			Handler: func(host moduleapi.Host, args string) {
				ctl, ok := host.(moduleapi.MCPController)
				if !ok {
					host.AddError("El host actual no expone MCP.")
					return
				}
				ctl.RunMCP(args)
			},
		}},
	})
}
