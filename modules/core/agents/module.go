package agents

import (
	"strings"

	"github.com/lilith/li/internal/moduleapi"
)

func init() {
	moduleapi.Register(moduleapi.Definition{
		ID: "core.agents", Name: "Agents", Version: "1", Source: "builtin", API: moduleapi.APIVersion,
		Description: "Subagentes, tareas y forks de trabajo.",
		Commands: []moduleapi.Command{
			{Name: "tasks", Order: 1000, Description: "Lista subagentes foreground/background de la sesión, estado, task_id y relación padre-hijo.", Handler: runTasks},
			{Name: "subtask", Usage: "[--foreground] [--worktree] <tarea>", Order: 1100, Description: "Crea un fork Claude-compatible que hereda la conversación, modelo, instrucciones y tools activos.", Handler: runSubtask},
			{Name: "agents", Aliases: []string{"subagents"}, Order: 1400, Description: "Lista subagentes Claude-compatible detectados; también puedes invocarlos con @nombre tarea.", Handler: runAgents},
		},
	})
}

func controller(host moduleapi.Host) (moduleapi.AgentController, bool) {
	ctl, ok := host.(moduleapi.AgentController)
	if !ok {
		host.AddError("El host actual no expone subagentes.")
	}
	return ctl, ok
}
func runTasks(host moduleapi.Host, _ string) {
	if ctl, ok := controller(host); ok {
		ctl.RunTasks()
	}
}
func runSubtask(host moduleapi.Host, args string) {
	if ctl, ok := controller(host); ok {
		ctl.RunSubtask(args)
	}
}
func runAgents(host moduleapi.Host, _ string) {
	ctl, ok := controller(host)
	if !ok {
		return
	}
	list := ctl.Agents()
	if len(list) == 0 {
		host.AddSystem("No hay subagentes disponibles.")
		return
	}
	var b strings.Builder
	b.WriteString("Subagentes disponibles:\n")
	visible := 0
	for _, a := range list {
		if a.Hidden {
			continue
		}
		visible++
		b.WriteString("\n@" + a.Name + " — " + a.Description + " [" + a.Source + "]")
	}
	if visible == 0 {
		b.WriteString("\n(no hay subagentes visibles; puede haber agentes hidden disponibles para delegación automática)")
	}
	b.WriteString("\n\nUso directo: @nombre <tarea>. El agente principal también puede delegar automáticamente con Agent.")
	host.AddSystem(b.String())
}
