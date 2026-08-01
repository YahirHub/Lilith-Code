package tui

import (
	"strings"

	"github.com/lilith/li/internal/tui/uikit"

	planstate "github.com/lilith/li/internal/plan"
)

// SlashCommand is a single "/foo" entry surfaced by the suggestion menu.
type SlashCommand struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Run         func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd
}

// Commands returns the full slash-command list.
func Commands() []SlashCommand {
	return []SlashCommand{
		{
			Name: "help", Aliases: []string{"h", "?"},
			Description: "Abre la referencia completa de comandos y atajos de teclado.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				return switchTo(NewHelpScreen(ctx))
			},
		},
		{
			Name:        "init",
			Description: "Analiza el proyecto y crea o mejora LILITH.md con instrucciones persistentes.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				return chat.runInit()
			},
		},
		{
			Name: "goal", Usage: "<objetivo>|status|pause|resume|complete|clear",
			Description: "Define o administra un objetivo persistente para trabajo autónomo de larga duración.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				return chat.runGoalCommand(args)
			},
		},
		{
			Name: "plan", Usage: "[show|status|exit|<instrucción>]",
			Description: "Activa Plan para explorar sin mutar; show muestra el plan listo y exit vuelve a Build.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				arg := strings.TrimSpace(args)
				switch strings.ToLower(arg) {
				case "show":
					if chat.plans == nil || strings.TrimSpace(chat.plans.LatestPlan()) == "" {
						chat.AddSystem("No hay un plan completado en esta sesión.")
					} else {
						chat.AddSystem("Plan actual:\n\n" + chat.plans.LatestPlan())
					}
					return nil
				case "status":
					chat.AddSystem("Agente seleccionado: " + strings.ToUpper(string(chat.selectedAgentMode())) + ". Tab recorre Build / Plan / Goal para el siguiente turno.")
					return nil
				case "exit", "off", "build":
					chat.setAgentMode(planstate.Build)
					return chat.chatMouseModeCmd()
				case "":
					chat.setAgentMode(planstate.Plan)
					return chat.chatMouseModeCmd()
				default:
					chat.setAgentMode(planstate.Plan)
					_, cmd := chat.submit(arg)
					return cmd
				}
			},
		},
		{
			Name:        "build",
			Description: "Selecciona Build para el siguiente turno y restaura herramientas de implementación.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				chat.setAgentMode(planstate.Build)
				return chat.chatMouseModeCmd()
			},
		},
		{
			Name: "compact", Usage: "[instrucciones opcionales]",
			Description: "Resume el historial antiguo, conserva los turnos recientes y libera contexto sin borrar el transcript.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				return chat.runCompactCommand(args)
			},
		},
		{
			Name:        "rewind",
			Description: "Restaura código, conversación o ambos al punto anterior a un mensaje del usuario.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				if chat != nil && chat.rewindSessionBusy() {
					chat.AddError("/rewind sólo puede ejecutarse cuando el agente, los comandos directos y las tareas en background están inactivos. Cancela o espera a que terminen y vuelve a intentarlo.")
					return nil
				}
				return switchTo(NewRewindScreen(ctx, chat))
			},
		},
		{
			Name:        "fork",
			Usage:       "[título opcional]",
			Description: "Crea una conversación y copia de archivos independientes; usa un git worktree cuando es posible.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				return chat.runForkSessionCommand(args)
			},
		},
		{
			Name: "memory", Usage: "[on|off|status]",
			Description: "Muestra instrucciones/memoria Claude-compatible y permite activar o desactivar auto memory.",
			Run:         func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd { return chat.runMemoryCommand(args) },
		},
		{
			Name: "mcp", Usage: "[reload]",
			Description: "Muestra servidores/herramientas MCP conectados o fuerza una reconexión.",
			Run:         func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd { return chat.runMCPCommand(args) },
		},
		{
			Name:        "tasks",
			Description: "Lista subagentes foreground/background de la sesión, estado, task_id y relación padre-hijo.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return chat.runTasksCommand() },
		},
		{
			Name: "subtask", Usage: "[--foreground] [--worktree] <tarea>",
			Description: "Crea un fork Claude-compatible que hereda la conversación, modelo, instrucciones y tools activos.",
			Run:         func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd { return chat.runForkCommand(args) },
		},
		{
			Name:        "plugins",
			Description: "Lista plugins locales Claude detectados y sus componentes namespaced.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return chat.runPluginsCommand() },
		},
		{
			Name:        "reload-plugins",
			Description: "Fuerza un rescan de plugins locales Claude para el siguiente turno.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return chat.runReloadPluginsCommand() },
		},
		{
			Name: "agents", Aliases: []string{"subagents"},
			Description: "Lista subagentes Claude-compatible detectados; también puedes invocarlos con @nombre tarea.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				list := chat.loadAgents()
				if len(list) == 0 {
					chat.AddSystem("No hay subagentes disponibles.")
					return nil
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
				chat.AddSystem(b.String())
				return nil
			},
		},
		{
			Name: "login", Aliases: []string{"signin"},
			Description: "Conecta un nuevo proveedor.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return switchTo(NewOnboarding(ctx, false)) },
		},
		{
			Name: "providers", Aliases: []string{"provider"},
			Description: "Administra proveedores configurados.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return switchTo(NewProviderScreen(ctx)) },
		},
		{
			Name: "models", Aliases: []string{"model"},
			Description: "Elige el modelo activo.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return switchTo(NewModelSelector(ctx)) },
		},
		{
			Name: "config", Aliases: []string{"settings"},
			Description: "Abre configuración de General, Búsqueda y Seguridad.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { return switchTo(NewConfigScreen(ctx)) },
		},
		{
			Name: "clear", Aliases: []string{"new", "c", "n", "reset"},
			Description: "Empieza una conversación nueva y vuelve al agente Build.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd { chat.Clear(); return uikit.DisableMouse },
		},
		{
			Name: "history", Aliases: []string{"chats", "resume", "continue"},
			Description: "Abre el historial de conversaciones del proyecto.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				return switchTo(NewHistory(ctx, chat.store, chat.project))
			},
		},
		{
			Name: "bash", Aliases: []string{"!"}, Usage: "<comando>",
			Description: "Ejecuta shell directamente con !comando; en Plan sólo admite inspección segura.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				if strings.TrimSpace(args) == "" {
					chat.AddSystem("Uso: !comando")
					return nil
				}
				_, cmd := chat.submit("!" + args)
				return cmd
			},
		},
		{
			Name:        "exit",
			Description: "Cierra Lilith. Es la única salida explícita del proceso.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) uikit.Cmd {
				if chat != nil {
					if chat.activeTurnID != 0 {
						chat.cancelTurn()
					}
					chat.runSessionHook("SessionEnd")
				}
				return uikit.Quit
			},
		},
	}
}

func FindCommand(name string) *SlashCommand {
	name = strings.ToLower(strings.TrimPrefix(name, "/"))
	all := Commands()
	for i := range all {
		if all[i].Name == name {
			c := all[i]
			return &c
		}
		for _, a := range all[i].Aliases {
			if a == name {
				c := all[i]
				return &c
			}
		}
	}
	return nil
}

func FilterCommands(query string) []SlashCommand {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	all := Commands()
	if q == "" {
		return all
	}
	out := []SlashCommand{}
	for _, c := range all {
		if _, ok := subsequenceMatch(c.Name, q); ok {
			out = append(out, c)
			continue
		}
		for _, a := range c.Aliases {
			if _, ok := subsequenceMatch(a, q); ok {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func helpText() string {
	var b strings.Builder
	for _, c := range Commands() {
		b.WriteString("/" + c.Name)
		if c.Usage != "" {
			b.WriteString(" " + c.Usage)
		}
		b.WriteString(" — " + c.Description + "\n")
	}
	return strings.TrimSpace(b.String())
}
