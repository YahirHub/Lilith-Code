package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SlashCommand is a single "/foo" entry surfaced by the suggestion menu.
type SlashCommand struct {
	Name        string
	Aliases     []string
	Description string
	Run         func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd
}

// Commands returns the full slash-command list (Codewolf-compatible names).
func Commands() []SlashCommand {
	return []SlashCommand{
		{
			Name: "help", Aliases: []string{"h", "?"},
			Description: "Muestra los comandos disponibles",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				chat.AddSystem(helpText())
				return nil
			},
		},
		{
			Name: "login", Aliases: []string{"signin"},
			Description: "Conecta un nuevo proveedor",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewOnboarding(ctx, false))
			},
		},
		{
			Name: "providers", Aliases: []string{"provider"},
			Description: "Lista los proveedores configurados",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				chat.AddSystem(listProviders(ctx))
				return nil
			},
		},
		{
			Name: "models", Aliases: []string{"model"},
			Description: "Elige el modelo activo",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewModelSelector(ctx))
			},
		},
		{
			Name: "config", Aliases: []string{"settings"},
			Description: "Abre la pantalla de configuración (skills, rutas, toggles)",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewConfigScreen(ctx))
			},
		},
		{
			Name: "clear", Aliases: []string{"new", "c", "n", "reset"},
			Description: "Empieza una conversación nueva",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				chat.Clear()
				return nil
			},
		},
		{
			Name: "history", Aliases: []string{"chats", "resume", "continue"},
			Description: "Abre el historial de conversaciones del proyecto",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return switchTo(NewHistory(ctx, chat.store, chat.project))
			},
		},
		{
			Name: "bash", Aliases: []string{"!"},
			Description: "Ejecuta un comando de shell (prefix: !)",
			Run: func(ctx *AppContext, chat *ChatModel, args string) tea.Cmd {
				chat.AddSystem("Modo bash llegará en la próxima fase. Prueba: !comando")
				return nil
			},
		},
		{
			Name: "exit", Aliases: []string{"quit", "q"},
			Description: "Cierra Lilith",
			Run: func(ctx *AppContext, chat *ChatModel, _ string) tea.Cmd {
				return tea.Quit
			},
		},
	}
}

// FindCommand looks up a command by exact name or alias.
func FindCommand(name string) *SlashCommand {
	name = strings.ToLower(strings.TrimPrefix(name, "/"))
	for i, cmd := range Commands() {
		if cmd.Name == name {
			c := Commands()[i]
			return &c
		}
		for _, a := range cmd.Aliases {
			if a == name {
				c := Commands()[i]
				return &c
			}
		}
	}
	return nil
}

// FilterCommands returns commands whose name/alias contain the (fuzzy) query.
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
	b.WriteString("Comandos disponibles:\n")
	for _, c := range Commands() {
		b.WriteString("  /")
		b.WriteString(c.Name)
		b.WriteString("  — ")
		b.WriteString(c.Description)
		b.WriteString("\n")
	}
	b.WriteString("\nAtajos: Enter enviar · ! prefix bash · Ctrl+R plegar razonamiento · Ctrl+C cancela tarea (2x sale)")
	return b.String()
}

func listProviders(ctx *AppContext) string {
	if len(ctx.Providers.Providers) == 0 {
		return "No hay proveedores configurados. Usa /login."
	}
	active := ctx.Providers.Active()
	var b strings.Builder
	b.WriteString("Proveedores configurados:\n")
	for _, p := range ctx.Providers.Providers {
		marker := "  "
		if p.ID == active.ProviderID {
			marker = "▸ "
		}
		b.WriteString(marker)
		b.WriteString(p.Name)
		b.WriteString("  (")
		b.WriteString(p.ID)
		b.WriteString(", ")
		b.WriteString(itoaInt(len(p.Models)))
		b.WriteString(" modelos)\n")
	}
	if active.ModelID != "" {
		b.WriteString("\nModelo activo: ")
		b.WriteString(active.ProviderName)
		b.WriteString(" / ")
		b.WriteString(active.ModelID)
	}
	return b.String()
}

func itoaInt(n int) string { return fmtInt(n) }
