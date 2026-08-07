package tui

import (
	"sort"
	"strings"
	"sync"

	_ "github.com/lilith/li/internal/distribution"
	"github.com/lilith/li/internal/moduleapi"
	"github.com/lilith/li/internal/tui/uikit"
)

type SlashItemKind string

const (
	SlashItemCommand SlashItemKind = "command"
	SlashItemSkill   SlashItemKind = "skill"
)

// SlashCommand is a single "/foo" entry surfaced by the suggestion menu.
type SlashCommand struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Kind        SlashItemKind
	Order       int
	Run         func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd
	ModuleID    string
}

var (
	modulesOnce sync.Once
	modulesReg  *moduleapi.Registry
)

// commandRegistry owns every slash command exposed by this build. Core and
// private commands are registered by statically linked modules; the TUI does
// not inject a compatibility mega-module anymore.
func commandRegistry() *moduleapi.Registry {
	modulesOnce.Do(func() {
		modulesReg = moduleapi.NewRegistry(moduleapi.Catalog(), nil)
	})
	return modulesReg
}

// Commands returns the full slash-command list contributed by enabled modules.
func Commands() []SlashCommand {
	reg := commandRegistry()
	items := reg.Commands()
	out := make([]SlashCommand, 0, len(items))
	for _, item := range items {
		item := item
		_, moduleID, _ := reg.FindCommand(item.Name)
		out = append(out, SlashCommand{
			Name:        item.Name,
			Aliases:     append([]string(nil), item.Aliases...),
			Usage:       item.Usage,
			Description: item.Description,
			Kind:        SlashItemCommand,
			Order:       item.Order,
			ModuleID:    moduleID,
			Run: func(ctx *AppContext, chat *ChatModel, args string) uikit.Cmd {
				host := newModuleHost(ctx, chat, reg)
				item.Handler(host, args)
				return host.cmd
			},
		})
	}
	return out
}

// FindModuleRoute resolves dynamic slash routes such as /skill:<name>.
func FindModuleRoute(name string) (moduleapi.Route, string, string, bool) {
	return commandRegistry().MatchRoute(name)
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
		if _, ok := slashMatchScore(c, q); ok {
			out = append(out, c)
		}
	}
	sortSlashRows(out, q)
	return out
}

// slashMatchScore ranks exact and prefix matches before fuzzy subsequences.
// This keeps /login above unrelated commands such as /reload-plugins even
// when both contain the requested letters in order.
func slashMatchScore(c SlashCommand, query string) (int, bool) {
	q := slashSearchQuery(query)
	if q == "" {
		return 0, true
	}
	candidates := append([]string{c.Name}, c.Aliases...)
	best := int(^uint(0) >> 1)
	matched := false
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		score, ok := slashCandidateScore(candidate, q)
		if ok && score < best {
			best, matched = score, true
		}
	}
	return best, matched
}

func slashSearchQuery(query string) string {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "/")))
	if _, _, target, ok := FindModuleRoute(q); ok {
		return strings.ToLower(strings.TrimSpace(target))
	}
	return q
}

func slashCandidateScore(candidate, query string) (int, bool) {
	if candidate == query {
		return 0, true
	}
	if strings.HasPrefix(candidate, query) {
		return 100 + len([]rune(candidate)) - len([]rune(query)), true
	}
	if index := strings.Index(candidate, query); index >= 0 {
		return 300 + index*8 + len([]rune(candidate)) - len([]rune(query)), true
	}
	indexes, ok := subsequenceMatch(candidate, query)
	if !ok {
		return 0, false
	}
	gaps := 0
	for i := 1; i < len(indexes); i++ {
		gaps += indexes[i] - indexes[i-1] - 1
	}
	start := 0
	if len(indexes) > 0 {
		start = indexes[0]
	}
	return 600 + start*12 + gaps*8 + len([]rune(candidate)), true
}

func sortSlashRows(rows []SlashCommand, query string) {
	q := slashSearchQuery(query)
	if q == "" {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := slashMatchScore(rows[i], q)
		right, _ := slashMatchScore(rows[j], q)
		if left != right {
			return left < right
		}
		leftKind, rightKind := normalizedSlashKind(rows[i]), normalizedSlashKind(rows[j])
		if leftKind != rightKind {
			return leftKind == SlashItemCommand
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
}

func normalizedSlashKind(row SlashCommand) SlashItemKind {
	if row.Kind == SlashItemSkill {
		return SlashItemSkill
	}
	return SlashItemCommand
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
