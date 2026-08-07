package moduleapi

import (
	"fmt"
	"sort"
	"strings"
)

type registeredCommand struct {
	moduleID string
	command  Command
}

type registeredRoute struct {
	moduleID string
	route    Route
	prefixes []string
}

// Registry validates linked modules and exposes only enabled contributions.
// Later modules never silently override existing slash commands or routes.
type Registry struct {
	statuses []Status
	commands []registeredCommand
	routes   []registeredRoute
	byToken  map[string]int
}

// NewRegistry builds a registry. reservedTokens are slash names/aliases still
// owned by the compatibility command layer; a module colliding with one is
// disabled rather than shadowing existing behavior.
func NewRegistry(defs []Definition, reservedTokens []string) *Registry {
	defs = append([]Definition(nil), defs...)
	sort.SliceStable(defs, func(i, j int) bool {
		return definitionPriority(defs[i]) < definitionPriority(defs[j])
	})
	r := &Registry{byToken: map[string]int{}}
	reserved := map[string]string{}
	for _, token := range reservedTokens {
		if key := normalizeToken(token); key != "" {
			reserved[key] = "core compatibility command /" + key
		}
	}

	statuses := make([]Status, len(defs))
	seenIDs := map[string]int{}
	for i, def := range defs {
		st := statusFromDefinition(def)
		statuses[i] = st
		id := normalizeToken(def.ID)
		if id == "" {
			disable(&statuses[i], "module id is empty")
			continue
		}
		statuses[i].ID = id
		if strings.HasPrefix(id, "core.") {
			source := strings.ToLower(strings.TrimSpace(def.Source))
			if source != "builtin" && source != "core" {
				disable(&statuses[i], "reserved core.* module id requires builtin/core source")
				continue
			}
		}
		if def.API != APIVersion {
			disable(&statuses[i], fmt.Sprintf("module API %d is incompatible with Lilith module API %d", def.API, APIVersion))
			continue
		}
		if prev, ok := seenIDs[id]; ok {
			disable(&statuses[i], fmt.Sprintf("duplicate module id; already provided by %s", statuses[prev].Name))
			continue
		}
		seenIDs[id] = i
	}

	// Dependencies are resolved before commands so a module whose prerequisite
	// is absent never reserves names that another valid module could use.
	changed := true
	for changed {
		changed = false
		for i, def := range defs {
			if !statuses[i].Enabled {
				continue
			}
			for _, dep := range def.Requires {
				depID := normalizeToken(dep)
				j, ok := seenIDs[depID]
				if !ok {
					disable(&statuses[i], "missing required module "+depID)
					changed = true
					break
				}
				if !statuses[j].Enabled {
					disable(&statuses[i], "required module "+depID+" is disabled")
					changed = true
					break
				}
			}
		}
	}

	// Resolve command and route collisions at module granularity. Disabling the
	// entire later module is predictable and keeps private merges fail-closed.
	claimed := map[string]string{}
	for token, owner := range reserved {
		claimed[token] = owner
	}
	claimedRoutes := map[string]string{}
	for i, def := range defs {
		if !statuses[i].Enabled {
			continue
		}
		if reason := definitionConflict(def, claimed, claimedRoutes); reason != "" {
			disable(&statuses[i], reason)
			continue
		}
		for _, cmd := range def.Commands {
			for _, token := range commandTokens(cmd) {
				claimed[token] = statuses[i].ID
			}
		}
		for _, route := range def.Routes {
			for _, prefix := range routePrefixes(route) {
				claimedRoutes[prefix] = statuses[i].ID
			}
		}
	}

	// A command-conflict can disable a dependency after the first dependency
	// pass. Propagate that state and rebuild indexes only from the final set.
	changed = true
	for changed {
		changed = false
		for i, def := range defs {
			if !statuses[i].Enabled {
				continue
			}
			for _, dep := range def.Requires {
				if j, ok := seenIDs[normalizeToken(dep)]; ok && !statuses[j].Enabled {
					disable(&statuses[i], "required module "+normalizeToken(dep)+" is disabled")
					changed = true
					break
				}
			}
		}
	}

	r.statuses = statuses
	for i, def := range defs {
		if !statuses[i].Enabled {
			continue
		}
		for _, cmd := range def.Commands {
			cmd.Name = normalizeToken(cmd.Name)
			cmd.Aliases = normalizeList(cmd.Aliases)
			idx := len(r.commands)
			r.commands = append(r.commands, registeredCommand{moduleID: statuses[i].ID, command: cmd})
			for _, token := range commandTokens(cmd) {
				r.byToken[token] = idx
			}
		}
		for _, route := range def.Routes {
			route.Prefix = normalizeToken(route.Prefix)
			route.Aliases = normalizeList(route.Aliases)
			r.routes = append(r.routes, registeredRoute{moduleID: statuses[i].ID, route: route, prefixes: routePrefixes(route)})
		}
	}
	sort.SliceStable(r.commands, func(i, j int) bool {
		left, right := commandOrder(r.commands[i].command), commandOrder(r.commands[j].command)
		if left != right {
			return left < right
		}
		if r.commands[i].moduleID != r.commands[j].moduleID {
			return r.commands[i].moduleID < r.commands[j].moduleID
		}
		return r.commands[i].command.Name < r.commands[j].command.Name
	})
	r.byToken = map[string]int{}
	for i, item := range r.commands {
		for _, token := range commandTokens(item.command) {
			r.byToken[token] = i
		}
	}

	// Longest prefix first makes future nested routes deterministic.
	sort.SliceStable(r.routes, func(i, j int) bool {
		return maxPrefixLen(r.routes[i].prefixes) > maxPrefixLen(r.routes[j].prefixes)
	})
	return r
}

func commandOrder(cmd Command) int {
	if cmd.Order <= 0 {
		return 10000
	}
	return cmd.Order
}

func definitionPriority(def Definition) int {
	if strings.HasPrefix(normalizeToken(def.ID), "core.") {
		return -100
	}
	source := strings.ToLower(strings.TrimSpace(def.Source))
	switch source {
	case "builtin", "core":
		return 0
	case "":
		return 50
	default:
		return 100
	}
}

func statusFromDefinition(def Definition) Status {
	api := def.API
	st := Status{
		ID:          normalizeToken(def.ID),
		Name:        strings.TrimSpace(def.Name),
		Version:     strings.TrimSpace(def.Version),
		Description: strings.TrimSpace(def.Description),
		Source:      strings.TrimSpace(def.Source),
		API:         api,
		Enabled:     true,
		Requires:    normalizeList(def.Requires),
		Optional:    normalizeList(def.Optional),
	}
	if st.Name == "" {
		st.Name = st.ID
	}
	if st.Version == "" {
		st.Version = "1"
	}
	if st.Source == "" {
		st.Source = "builtin"
	}
	for _, cmd := range def.Commands {
		if name := normalizeToken(cmd.Name); name != "" {
			st.Commands = append(st.Commands, "/"+name)
		}
	}
	for _, route := range def.Routes {
		for _, prefix := range routePrefixes(route) {
			if prefix != "" {
				st.Routes = append(st.Routes, "/"+prefix+"*")
			}
		}
	}
	return st
}

func disable(st *Status, reason string) {
	st.Enabled = false
	if st.Reason == "" {
		st.Reason = reason
	}
}

func definitionConflict(def Definition, claimed, routes map[string]string) string {
	if len(def.Commands) == 0 && len(def.Routes) == 0 {
		return "module provides no commands or routes"
	}
	local := map[string]bool{}
	for _, cmd := range def.Commands {
		if normalizeToken(cmd.Name) == "" || cmd.Handler == nil {
			return "module contains an invalid command"
		}
		for _, token := range commandTokens(cmd) {
			if local[token] {
				return "duplicate command token /" + token + " inside module"
			}
			local[token] = true
			if owner, ok := claimed[token]; ok {
				return fmt.Sprintf("slash command /%s conflicts with %s", token, owner)
			}
			for prefix, owner := range routes {
				if strings.HasPrefix(token, prefix) {
					return fmt.Sprintf("slash command /%s falls inside route /%s* owned by %s", token, prefix, owner)
				}
			}
		}
	}
	localRoutes := map[string]bool{}
	for _, route := range def.Routes {
		if normalizeToken(route.Prefix) == "" || route.Handler == nil {
			return "module contains an invalid route"
		}
		for _, prefix := range routePrefixes(route) {
			if !strings.HasSuffix(prefix, ":") {
				return "dynamic slash route /" + prefix + "* must end with ':'"
			}
			if localRoutes[prefix] {
				return "duplicate route /" + prefix + "* inside module"
			}
			localRoutes[prefix] = true
			if owner, ok := routes[prefix]; ok {
				return fmt.Sprintf("slash route /%s* conflicts with %s", prefix, owner)
			}
			for token, owner := range claimed {
				if strings.HasPrefix(token, prefix) {
					return fmt.Sprintf("slash route /%s* contains command /%s owned by %s", prefix, token, owner)
				}
			}
		}
	}
	for token := range local {
		for prefix := range localRoutes {
			if strings.HasPrefix(token, prefix) {
				return fmt.Sprintf("slash command /%s overlaps route /%s* inside module", token, prefix)
			}
		}
	}
	return ""
}

func commandTokens(cmd Command) []string {
	return append([]string{normalizeToken(cmd.Name)}, normalizeList(cmd.Aliases)...)
}

func routePrefixes(route Route) []string {
	return append([]string{normalizeToken(route.Prefix)}, normalizeList(route.Aliases)...)
}

func normalizeList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, item := range in {
		item = normalizeToken(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func maxPrefixLen(prefixes []string) int {
	max := 0
	for _, prefix := range prefixes {
		if len(prefix) > max {
			max = len(prefix)
		}
	}
	return max
}

// Commands returns exact commands in deterministic registration order.
func (r *Registry) Commands() []Command {
	if r == nil {
		return nil
	}
	out := make([]Command, 0, len(r.commands))
	for _, item := range r.commands {
		out = append(out, item.command)
	}
	return out
}

// FindCommand resolves a command name or alias without a leading slash.
func (r *Registry) FindCommand(name string) (Command, string, bool) {
	if r == nil {
		return Command{}, "", false
	}
	idx, ok := r.byToken[normalizeToken(name)]
	if !ok || idx < 0 || idx >= len(r.commands) {
		return Command{}, "", false
	}
	item := r.commands[idx]
	return item.command, item.moduleID, true
}

// MatchRoute resolves a dynamic prefix and returns the route plus target.
func (r *Registry) MatchRoute(name string) (Route, string, string, bool) {
	if r == nil {
		return Route{}, "", "", false
	}
	name = normalizeToken(name)
	for _, item := range r.routes {
		for _, prefix := range item.prefixes {
			if strings.HasPrefix(name, prefix) {
				target := strings.TrimSpace(strings.TrimPrefix(name, prefix))
				return item.route, item.moduleID, target, true
			}
		}
	}
	return Route{}, "", "", false
}

// Statuses returns an isolated module-state snapshot sorted by ID.
func (r *Registry) Statuses() []Status {
	if r == nil {
		return nil
	}
	out := make([]Status, len(r.statuses))
	for i, st := range r.statuses {
		out[i] = st
		out[i].Requires = append([]string(nil), st.Requires...)
		out[i].Optional = append([]string(nil), st.Optional...)
		out[i].Commands = append([]string(nil), st.Commands...)
		out[i].Routes = append([]string(nil), st.Routes...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
