package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const validationOutputLimit = 24 << 10

type validationCommand struct {
	name string
	dir  string
	exe  string
	args []string
}

// Validate chooses formatter/compiler/test commands from actual manifests and
// installed tools. It never installs dependencies and only formats when
// ApplyFormat is explicitly true.
func (m *Manager) Validate(ctx context.Context, options ValidationOptions) ValidationResult {
	profile := m.RefreshProfile()
	result := ValidationResult{Profile: profile, Successful: true}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Minute
	}
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = "quick"
	}
	commands := m.validationCommands(profile, options, mode)
	if len(commands) == 0 {
		result.Steps = append(result.Steps, ValidationStep{Name: "project validation", Skipped: true, Reason: "no compatible compiler, formatter or test runner was detected", Successful: true})
		return result
	}
	for _, command := range commands {
		step := runValidationCommand(ctx, command, options.Timeout)
		result.Steps = append(result.Steps, step)
		if !step.Successful {
			result.Successful = false
			// Formatting failures should not suppress compiler/test diagnostics, but
			// cancellation should stop immediately.
			if ctx.Err() != nil {
				break
			}
		}
	}
	return result
}

func (m *Manager) validationCommands(profile Profile, options ValidationOptions, mode string) []validationCommand {
	ctx := adapterContext{manager: m, profile: profile, options: options, mode: mode, changed: normalizeChangedPaths(m.root, options.ChangedPaths)}
	var commands []validationCommand
	for _, adapter := range languageAdapters {
		if !adapterEnabled(adapter, profile, m.root) {
			continue
		}
		commands = append(commands, adapter.Commands(ctx)...)
	}
	return dedupeValidationCommands(commands)
}

func nodeValidationCommands(root, manager string, tools map[string]string, mode string) []validationCommand {
	manifest := filepath.Join(root, "package.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	exe := tools[manager]
	if exe == "" {
		for _, candidate := range []string{"bun", "pnpm", "yarn", "npm"} {
			if tools[candidate] != "" {
				manager, exe = candidate, tools[candidate]
				break
			}
		}
	}
	if exe == "" {
		return nil
	}
	var names []string
	for _, name := range []string{"format:check", "lint", "typecheck", "check"} {
		if pkg.Scripts[name] != "" {
			names = append(names, name)
		}
	}
	if mode == "full" && pkg.Scripts["test"] != "" {
		names = append(names, "test")
	}
	var out []validationCommand
	for _, name := range names {
		args := []string{"run", name}
		if manager == "yarn" {
			args = []string{name}
		}
		out = append(out, validationCommand{name: manager + " " + name, dir: root, exe: exe, args: args})
	}
	return out
}

func packageJSONHasScript(path, name string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, ok := pkg.Scripts[name]
	return ok
}

func runValidationCommand(parent context.Context, command validationCommand, timeout time.Duration) ValidationStep {
	step := ValidationStep{Name: command.name, Command: formatCommand(command.exe, command.args), Dir: command.dir, ExitCode: -1}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, command.exe, command.args...)
	cmd.Dir = command.dir
	output := limitedBuffer{limit: validationOutputLimit}
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	step.Duration = time.Since(start)
	step.Output = truncateValidationOutput(output.String())
	if err == nil {
		step.ExitCode = 0
		step.Successful = true
		// gofmt -l is a check whose non-empty stdout means formatting differs.
		if strings.Contains(strings.ToLower(command.name), "gofmt check") && strings.TrimSpace(step.Output) != "" {
			step.Successful = false
			step.ExitCode = 1
		}
		return step
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		step.ExitCode = exitErr.ExitCode()
	} else if ctx.Err() != nil {
		step.Reason = ctx.Err().Error()
	} else {
		step.Reason = err.Error()
	}
	return step
}

func truncateValidationOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= validationOutputLimit {
		return value
	}
	head := validationOutputLimit / 3
	tail := validationOutputLimit - head
	return value[:head] + fmt.Sprintf("\n... %d bytes omitted ...\n", len(value)-validationOutputLimit) + value[len(value)-tail:]
}

func formatCommand(exe string, args []string) string {
	parts := []string{filepath.Base(exe)}
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"") {
			parts = append(parts, fmt.Sprintf("%q", arg))
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}

func normalizeChangedPaths(root string, paths []string) []string {
	seen := map[string]bool{}
	var out []string
	root, _ = filepath.Abs(root)
	root = filepath.Clean(root)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = root
	}
	resolvedRoot = filepath.Clean(resolvedRoot)
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		absolute := value
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, absolute)
		}
		absolute, err = filepath.Abs(absolute)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		rel, err := filepath.Rel(root, absolute)
		if pathEscapesRoot(rel, err) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			// Deleted and not-yet-created paths are not valid formatter/compiler
			// inputs and are deliberately omitted.
			continue
		}
		resolvedRel, relErr := filepath.Rel(resolvedRoot, filepath.Clean(resolved))
		if pathEscapesRoot(resolvedRel, relErr) {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		rel = filepath.Clean(rel)
		if rel == "." || seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func pathEscapesRoot(rel string, err error) bool {
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func filterExt(paths []string, extensions ...string) []string {
	set := map[string]bool{}
	for _, ext := range extensions {
		set[strings.ToLower(ext)] = true
	}
	var out []string
	for _, path := range paths {
		if set[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
	}
	return out
}

func goPackagesForChanged(root string, paths []string) []string {
	set := map[string]bool{}
	for _, path := range filterExt(paths, ".go") {
		dir := filepath.Dir(path)
		if dir == "." {
			set["."] = true
		} else {
			set["./"+filepath.ToSlash(dir)] = true
		}
	}
	out := make([]string, 0, len(set))
	for pkg := range set {
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out
}

func dedupeValidationCommands(commands []validationCommand) []validationCommand {
	seen := map[string]bool{}
	out := commands[:0]
	for _, command := range commands {
		key := command.dir + "\x00" + command.exe + "\x00" + strings.Join(command.args, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, command)
	}
	return out
}

func ternary[T any](condition bool, yes, no T) T {
	if condition {
		return yes
	}
	return no
}
