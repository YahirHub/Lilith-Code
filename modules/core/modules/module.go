package modules

import (
	"sort"
	"strconv"
	"strings"

	"github.com/lilith/li/internal/moduleapi"
)

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.modules", Name: "Module Registry", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Inspección de módulos enlazados y sus capacidades slash.",
		Commands:    []moduleapi.Command{{Name: "modules", Order: 150, Usage: "[id]", Description: "Lista módulos enlazados o muestra el diagnóstico de uno.", Handler: runModules}},
	})
}

func runModules(host moduleapi.Host, args string) {
	statuses := host.ModuleStatuses()
	query := strings.ToLower(strings.TrimSpace(args))
	if query != "" {
		for _, st := range statuses {
			if strings.EqualFold(st.ID, query) || strings.EqualFold(st.Name, query) {
				host.AddSystem(formatDetail(st))
				return
			}
		}
		host.AddError("Módulo no encontrado: " + strings.TrimSpace(args))
		return
	}
	if len(statuses) == 0 {
		host.AddSystem("No hay módulos enlazados en esta distribución.")
		return
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	var b strings.Builder
	b.WriteString("Módulos enlazados (Module API " + strconv.Itoa(moduleapi.APIVersion) + "):\n")
	for _, st := range statuses {
		state := "✓"
		if !st.Enabled {
			state = "✗"
		}
		b.WriteString("\n" + state + " " + st.ID + " · " + st.Name + " v" + st.Version + " [" + st.Source + "]")
		if !st.Enabled {
			b.WriteString("\n  deshabilitado: " + st.Reason)
			continue
		}
		if len(st.Commands) > 0 {
			b.WriteString("\n  comandos: " + strings.Join(st.Commands, ", "))
		}
		if len(st.Routes) > 0 {
			b.WriteString("\n  rutas: " + strings.Join(st.Routes, ", "))
		}
	}
	b.WriteString("\n\nUsa /modules <id> para ver dependencias y diagnóstico.")
	host.AddSystem(b.String())
}

func formatDetail(st moduleapi.Status) string {
	var b strings.Builder
	b.WriteString(st.ID + " — " + st.Name + "\nversión: " + st.Version + "\nsource: " + st.Source + "\nmodule API: " + strconv.Itoa(st.API) + "\nestado: ")
	if st.Enabled {
		b.WriteString("enabled")
	} else {
		b.WriteString("disabled — " + st.Reason)
	}
	if st.Description != "" {
		b.WriteString("\n\n" + st.Description)
	}
	if len(st.Requires) > 0 {
		b.WriteString("\nrequires: " + strings.Join(st.Requires, ", "))
	}
	if len(st.Optional) > 0 {
		b.WriteString("\noptional: " + strings.Join(st.Optional, ", "))
	}
	if len(st.Commands) > 0 {
		b.WriteString("\ncommands: " + strings.Join(st.Commands, ", "))
	}
	if len(st.Routes) > 0 {
		b.WriteString("\nroutes: " + strings.Join(st.Routes, ", "))
	}
	return b.String()
}
