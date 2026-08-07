// Package moduleapi defines Lilith's stable in-process module contract.
//
// Modules are compiled into the same Go binary. They are not dynamic .so/.dll
// plugins and therefore preserve static, CGO-free builds on every supported OS.
package moduleapi

import "strings"

// APIVersion is the contract version implemented by this Lilith build.
// Private distributions should pin their modules to this value and fail closed
// when the contract changes instead of relying on internal TUI implementation.
const APIVersion = 1

// Screen identifies a core screen that a module may request without importing
// internal/tui. Keep this list deliberately small; company modules should use
// stable Host operations rather than depend on ChatModel internals.
type Screen string

const (
	ScreenRewind Screen = "rewind"
)

// Host is the deliberately small stable boundary exposed to modules. Optional
// features live in capability interfaces below so adding a new capability does
// not force every private module to implement a larger interface after merging
// a newer public main. Modules never receive *ChatModel.
type Host interface {
	ConfigDir() string
	ProjectRoot() string
	AddSystem(text string)
	AddError(text string)
	ModuleStatuses() []Status
}

// SkillInvoker is implemented by hosts that can invoke Lilith Agent Skills.
type SkillInvoker interface {
	InvokeSkill(name, args string)
}

// Submitter is implemented by hosts that can submit another chat instruction.
type Submitter interface {
	Submit(text string)
}

// ScreenOpener is implemented by interactive hosts that expose known core
// screens without leaking TUI implementation types into modules.
type ScreenOpener interface {
	OpenScreen(screen Screen)
}

// RewindState exposes only the readiness check required by core.rewind.
type RewindState interface {
	RewindBusy() bool
}

// CommandHandler handles an exact slash command. The Host collects any UI
// command produced by operations such as OpenScreen/InvokeSkill/Submit.
type CommandHandler func(host Host, args string)

// RouteHandler handles a dynamic slash prefix such as /skill:<name>.
// target contains the portion after the matched prefix.
type RouteHandler func(host Host, target, args string)

// Command is an exact slash-command contribution from one module.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Order       int
	Handler     CommandHandler
}

// Route is a dynamic slash-command prefix contribution. Prefix must not include
// the leading '/'. A route with Prefix "skill:" matches /skill:frontend.
type Route struct {
	Prefix      string
	Aliases     []string
	Usage       string
	Description string
	Kind        string
	Handler     RouteHandler
}

// Definition describes one statically linked module.
type Definition struct {
	ID          string
	Name        string
	Version     string
	Description string
	Source      string
	API         int
	Requires    []string
	Optional    []string
	Commands    []Command
	Routes      []Route
}

// Status is safe diagnostic metadata exposed to /modules.
type Status struct {
	ID          string
	Name        string
	Version     string
	Description string
	Source      string
	API         int
	Enabled     bool
	Reason      string
	Requires    []string
	Optional    []string
	Commands    []string
	Routes      []string
}

func normalizeToken(v string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(v, "/")))
}
