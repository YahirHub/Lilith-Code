package plan

import (
	"errors"
	"regexp"
	"strings"
)

var unsafeShellSyntax = regexp.MustCompile(`[\n\r;&|<>` + "`" + `]|\$\(|\$\{|>\s*|<\s*`)

// ToolVisible applies the hard Plan-mode capability ceiling. The terminal tool
// stays visible because it has a second command-level read-only validator.
func ToolVisible(mode Mode, name string, mutating bool) bool {
	if mode != Plan {
		return name != "plan_question" && name != "plan_exit"
	}
	switch name {
	case "plan_question", "plan_exit", "run_terminal_command":
		return true
	case "todo_write":
		return false
	}
	return !mutating
}

func ValidateTool(mode Mode, name string, mutating bool, args map[string]any) error {
	if mode != Plan {
		return nil
	}
	if !ToolVisible(mode, name, mutating) {
		return errors.New("Plan mode is read-only: tool is blocked until you switch back to Build with Tab")
	}
	if name == "run_terminal_command" {
		command, _ := args["command"].(string)
		if !IsSafeCommand(command) {
			return errors.New("Plan mode blocks this shell command because it is not in the read-only inspection allowlist")
		}
	}
	return nil
}

// IsSafeCommand intentionally accepts a small, boring subset. Plan mode can
// already inspect files through native tools, so shell access is only an escape
// hatch for read-only project metadata. Compound shell syntax and redirection
// are rejected rather than trying to prove arbitrary shell programs harmless.
func IsSafeCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || unsafeShellSyntax.MatchString(command) {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := strings.ToLower(fields[0])
	args := fields[1:]
	switch base {
	case "pwd", "whoami", "hostname":
		return len(args) == 0
	case "ls", "dir":
		return safeSimpleArgs(args, nil)
	case "cat", "type", "head", "tail", "wc":
		return safeSimpleArgs(args, map[string]bool{"-f": false})
	case "rg", "ripgrep":
		return safeRipgrepArgs(args)
	case "git":
		return safeGitArgs(args)
	case "go":
		return safeGoArgs(args)
	case "node", "bun", "npm", "pnpm", "yarn", "python", "python3":
		return len(args) == 1 && (args[0] == "--version" || args[0] == "-v" || args[0] == "version")
	default:
		return false
	}
}

func safeSimpleArgs(args []string, denied map[string]bool) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "=") && strings.HasPrefix(lower, "--output") {
			return false
		}
		if denied != nil {
			if _, ok := denied[lower]; ok {
				return false
			}
		}
	}
	return true
}

func safeRipgrepArgs(args []string) bool {
	for i, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "--pre" || strings.HasPrefix(lower, "--pre=") || lower == "--pre-glob" || strings.HasPrefix(lower, "--pre-glob=") {
			return false
		}
		if (lower == "--replace" || lower == "-r") && i+1 < len(args) {
			// --replace only transforms stdout and is not a write, but excluding it
			// keeps the Plan allowlist inspection-only and easy to reason about.
			return false
		}
	}
	return true
}

func safeGitArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Permit harmless global flags before the subcommand.
	i := 0
	for i < len(args) {
		a := strings.ToLower(args[i])
		if a == "--no-pager" || a == "--paginate" || strings.HasPrefix(a, "-c") {
			// -c can change command behavior (including external diff), so reject it.
			if strings.HasPrefix(a, "-c") {
				return false
			}
			i++
			continue
		}
		break
	}
	if i >= len(args) {
		return false
	}
	sub := strings.ToLower(args[i])
	rest := args[i+1:]
	switch sub {
	case "status", "log", "show", "rev-parse", "ls-files", "grep", "describe", "shortlog":
		return noGitMutatingFlags(rest)
	case "diff":
		return noGitMutatingFlags(rest)
	case "branch":
		if len(rest) == 0 {
			return true
		}
		for _, a := range rest {
			lower := strings.ToLower(a)
			if lower == "--list" || lower == "--show-current" || lower == "-a" || lower == "--all" || lower == "-r" || lower == "--remotes" || lower == "-v" || lower == "-vv" || strings.HasPrefix(lower, "--contains") || strings.HasPrefix(lower, "--no-contains") || strings.HasPrefix(lower, "--merged") || strings.HasPrefix(lower, "--no-merged") {
				continue
			}
			return false
		}
		return true
	case "remote":
		return len(rest) == 0 || (len(rest) == 1 && (rest[0] == "-v" || rest[0] == "--verbose"))
	default:
		return false
	}
}

func noGitMutatingFlags(args []string) bool {
	for _, a := range args {
		lower := strings.ToLower(a)
		if lower == "--ext-diff" || strings.HasPrefix(lower, "--output=") || lower == "--output" || lower == "--exec" || strings.HasPrefix(lower, "--exec=") {
			return false
		}
	}
	return true
}

func safeGoArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "version":
		return len(rest) == 0
	case "env":
		for _, a := range rest {
			lower := strings.ToLower(a)
			if lower == "-w" || lower == "-u" || strings.HasPrefix(lower, "-w=") || strings.HasPrefix(lower, "-u=") {
				return false
			}
		}
		return true
	case "list", "doc":
		return true
	default:
		return false
	}
}
