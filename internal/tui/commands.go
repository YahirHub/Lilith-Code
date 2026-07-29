package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	planstate "github.com/lilith/li/internal/plan"
)

// SlashCommand is a single "/foo" entry surfaced by the suggestion menu.
type SlashCommand struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Run         func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd
}

// Commands returns the full slash-command list.
func Commands() []SlashCommand {
	return []SlashCommand{
		{
			Name: "help", Aliases: []string{"h", "?"},
			Description: "Abre la referencia completa de comandos y atajos de teclado.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewHelpScreen(ctx))
			},
		},
		{
			Name: "plan", Usage: "[show|status|exit|<instrucción>]",
			Description: "Activa Plan para explorar sin mutar; show muestra el plan listo y exit vuelve a Build.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd {
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
					chat.AddSystem("Agente seleccionado: " + strings.ToUpper(string(chat.selectedAgentMode())) + ". Tab alterna Build / Plan para el siguiente turno.")
					return nil
				case "exit", "off", "build":
					chat.setAgentMode(planstate.Build)
					return nil
				case "":
					chat.setAgentMode(planstate.Plan)
					return nil
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
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				chat.setAgentMode(planstate.Build)
				return nil
			},
		},
		{
			Name: "login", Aliases: []string{"signin"},
			Description: "Conecta un nuevo proveedor.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd { return switchTo(NewOnboarding(ctx, false)) },
		},
		{
			Name: "providers", Aliases: []string{"provider"},
			Description: "Administra proveedores configurados.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd { return switchTo(NewProviderScreen(ctx)) },
		},
		{
			Name: "models", Aliases: []string{"model"},
			Description: "Elige el modelo activo.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd { return switchTo(NewModelSelector(ctx)) },
		},
		{
			Name: "config", Aliases: []string{"settings"},
			Description: "Abre configuración de General, Búsqueda y Seguridad.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd { return switchTo(NewConfigScreen(ctx)) },
		},
		{
			Name: "clear", Aliases: []string{"new", "c", "n", "reset"},
			Description: "Empieza una conversación nueva y vuelve al agente Build.",
			Run:         func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd { chat.Clear(); return nil },
		},
		{
			Name: "history", Aliases: []string{"chats", "resume", "continue"},
			Description: "Abre el historial de conversaciones del proyecto.",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewHistory(ctx, chat.store, chat.project))
			},
		},
		{
			Name: "bash", Aliases: []string{"!"}, Usage: "<comando>",
			Description: "Ejecuta shell directamente con !comando; en Plan sólo admite inspección segura.",
			Run: func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd {
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
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				if chat != nil && chat.activeTurnID != 0 {
					chat.cancelTurn()
				}
				return tea.Quit
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
