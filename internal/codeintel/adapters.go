package codeintel

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const pythonASTValidationScript = `import ast
import pathlib
import sys

ignored = {".git", ".venv", "venv", "node_modules", "build", "dist", "__pycache__"}
paths = [pathlib.Path(value) for value in sys.argv[1:]]
if not paths:
    paths = [
        path
        for path in pathlib.Path(".").rglob("*.py")
        if not any(part in ignored for part in path.parts)
    ]
errors = []
for path in paths:
    try:
        ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    except Exception as exc:
        errors.append(f"{path}: {exc}")
if errors:
    print("\n".join(errors))
    raise SystemExit(1)
`

// LanguageAdapter selects project-native formatters, compilers and tests for a
// detected ecosystem. Adapters only use tools already present on the host.
type LanguageAdapter interface {
	Name() string
	Matches(Project) bool
	Commands(adapterContext) []validationCommand
}

type adapterContext struct {
	manager *Manager
	profile Profile
	options ValidationOptions
	mode    string
	changed []string
}

type functionAdapter struct {
	name     string
	kinds    []string
	commands func(adapterContext) []validationCommand
}

func (a functionAdapter) Name() string { return a.name }
func (a functionAdapter) Matches(project Project) bool {
	for _, actual := range project.Kinds {
		for _, expected := range a.kinds {
			if actual == expected {
				return true
			}
		}
	}
	return false
}
func (a functionAdapter) Commands(ctx adapterContext) []validationCommand { return a.commands(ctx) }

var languageAdapters = []LanguageAdapter{
	functionAdapter{name: "go", kinds: []string{"go", "go-workspace"}, commands: goAdapterCommands},
	functionAdapter{name: "rust", kinds: []string{"rust"}, commands: rustAdapterCommands},
	functionAdapter{name: "node", kinds: []string{"node", "typescript"}, commands: nodeAdapterCommands},
	functionAdapter{name: "deno", kinds: []string{"deno"}, commands: denoAdapterCommands},
	functionAdapter{name: "python", kinds: []string{"python"}, commands: pythonAdapterCommands},
	functionAdapter{name: "php", kinds: []string{"php", "laravel"}, commands: phpAdapterCommands},
	functionAdapter{name: "ruby", kinds: []string{"ruby"}, commands: rubyAdapterCommands},
	functionAdapter{name: "dart", kinds: []string{"dart"}, commands: dartAdapterCommands},
	functionAdapter{name: "swift", kinds: []string{"swift"}, commands: swiftAdapterCommands},
	functionAdapter{name: "elixir", kinds: []string{"elixir"}, commands: elixirAdapterCommands},
	functionAdapter{name: "dotnet", kinds: []string{"dotnet"}, commands: dotnetAdapterCommands},
	functionAdapter{name: "godot", kinds: []string{"godot"}, commands: godotAdapterCommands},
	functionAdapter{name: "maven", kinds: []string{"maven"}, commands: mavenAdapterCommands},
	functionAdapter{name: "gradle", kinds: []string{"gradle"}, commands: gradleAdapterCommands},
	functionAdapter{name: "cmake", kinds: []string{"cmake"}, commands: cmakeAdapterCommands},
	functionAdapter{name: "make", kinds: []string{"make"}, commands: makeAdapterCommands},
}

func availableAdapterNames(project Project) []string {
	profile := Profile{Environment: detectEnvironment(), Project: project}
	return availableAdapterNamesFor(profile, project.Root)
}

func availableAdapterNamesFor(profile Profile, root string) []string {
	var names []string
	for _, adapter := range languageAdapters {
		if adapterEnabled(adapter, profile, root) {
			names = append(names, adapter.Name())
		}
	}
	return names
}

func adapterEnabled(adapter LanguageAdapter, profile Profile, root string) bool {
	if !adapter.Matches(profile.Project) {
		return false
	}
	if adapter.Name() != "make" || !strings.EqualFold(profile.Environment.OS, "windows") {
		return true
	}
	// A secondary Makefile must not override a native ecosystem adapter on
	// Windows. Most such files use POSIX environment assignments and utilities
	// that cmd.exe cannot execute even when make.exe itself exists.
	for _, kind := range profile.Project.Kinds {
		if kind != "make" {
			return false
		}
	}
	return windowsMakefileCompatible(root)
}

func windowsMakefileCompatible(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	for _, token := range []string{"mkdir -p", "rm -f", "/dev/null", "chmod +x", "export ", "cgo_enabled=", "goos=", "goarch="} {
		if strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func kindSet(project Project) map[string]bool {
	out := make(map[string]bool, len(project.Kinds))
	for _, kind := range project.Kinds {
		out[kind] = true
	}
	return out
}

func goAdapterCommands(ctx adapterContext) []validationCommand {
	root, tools := ctx.manager.root, ctx.profile.Environment.Tools
	var commands []validationCommand
	goFiles := filterExt(ctx.changed, ".go")
	if gofmt := tools["gofmt"]; gofmt != "" && len(goFiles) > 0 {
		args := append([]string(nil), goFiles...)
		if ctx.options.ApplyFormat {
			args = append([]string{"-w"}, args...)
		} else {
			args = append([]string{"-l"}, args...)
		}
		commands = append(commands, validationCommand{name: ternary(ctx.options.ApplyFormat, "gofmt apply", "gofmt check"), dir: root, exe: gofmt, args: args})
	}
	if goExe := tools["go"]; goExe != "" {
		if ctx.mode == "full" {
			commands = append(commands,
				validationCommand{name: "go test", dir: root, exe: goExe, args: []string{"test", "./..."}},
				validationCommand{name: "go vet", dir: root, exe: goExe, args: []string{"vet", "./..."}},
			)
		} else {
			packages := goPackagesForChanged(root, ctx.changed)
			if len(packages) == 0 {
				packages = []string{"./..."}
			}
			commands = append(commands, validationCommand{name: "go test", dir: root, exe: goExe, args: append([]string{"test"}, packages...)})
		}
	}
	return commands
}

func rustAdapterCommands(ctx adapterContext) []validationCommand {
	cargo := ctx.profile.Environment.Tools["cargo"]
	if cargo == "" {
		return nil
	}
	var commands []validationCommand
	rustFiles := filterExt(ctx.changed, ".rs")
	if ctx.options.ApplyFormat {
		// cargo fmt formats a package/workspace, not a precise file list. Use the
		// standalone rustfmt executable only for explicitly selected source files.
		if rustfmt := ctx.profile.Environment.Tools["rustfmt"]; rustfmt != "" && len(rustFiles) > 0 {
			args := rustfmtArgs(ctx.manager.root, rustFiles)
			commands = append(commands, validationCommand{name: "rustfmt apply", dir: ctx.manager.root, exe: rustfmt, args: args})
		}
	} else {
		commands = append(commands, validationCommand{name: "cargo fmt check", dir: ctx.manager.root, exe: cargo, args: []string{"fmt", "--", "--check"}})
	}
	commands = append(commands, validationCommand{name: "cargo check", dir: ctx.manager.root, exe: cargo, args: []string{"check", "--workspace", "--all-targets"}})
	if ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "cargo test", dir: ctx.manager.root, exe: cargo, args: []string{"test", "--workspace"}})
	}
	return commands
}

func rustfmtArgs(root string, paths []string) []string {
	args := []string{}
	if edition := cargoEdition(filepath.Join(root, "Cargo.toml")); edition != "" {
		args = append(args, "--edition", edition)
	}
	return append(args, paths...)
}

func cargoEdition(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "edition") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		switch value {
		case "2015", "2018", "2021", "2024":
			return value
		}
	}
	return ""
}

func nodeAdapterCommands(ctx adapterContext) []validationCommand {
	return nodeValidationCommands(ctx.manager.root, ctx.profile.Project.PackageManager, ctx.profile.Environment.Tools, ctx.mode)
}

func denoAdapterCommands(ctx adapterContext) []validationCommand {
	deno := ctx.profile.Environment.Tools["deno"]
	if deno == "" {
		return nil
	}
	files := filterExt(ctx.changed, ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs")
	var commands []validationCommand
	if ctx.options.ApplyFormat {
		if len(files) > 0 {
			commands = append(commands, validationCommand{name: "deno fmt apply", dir: ctx.manager.root, exe: deno, args: append([]string{"fmt"}, files...)})
		}
	} else {
		args := []string{"fmt", "--check"}
		if len(files) > 0 && ctx.mode != "full" {
			args = append(args, files...)
		}
		commands = append(commands, validationCommand{name: "deno fmt check", dir: ctx.manager.root, exe: deno, args: args})
	}
	checkFiles := files
	if len(checkFiles) == 0 {
		checkFiles = sourceFiles(ctx.manager.root, 200, ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs")
	}
	if len(checkFiles) > 0 {
		commands = append(commands, validationCommand{name: "deno check", dir: ctx.manager.root, exe: deno, args: append([]string{"check"}, checkFiles...)})
	}
	if ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "deno test", dir: ctx.manager.root, exe: deno, args: []string{"test"}})
	}
	return commands
}

func pythonAdapterCommands(ctx adapterContext) []validationCommand {
	root, tools := ctx.manager.root, ctx.profile.Environment.Tools
	python := tools["python3"]
	if python == "" {
		python = tools["python"]
	}
	pyFiles := filterExt(ctx.changed, ".py", ".pyi")
	var commands []validationCommand
	if ruff := tools["ruff"]; ruff != "" {
		if ctx.options.ApplyFormat && len(pyFiles) > 0 {
			commands = append(commands, validationCommand{name: "ruff format apply", dir: root, exe: ruff, args: append([]string{"format"}, pyFiles...)})
		}
		args := []string{"check", "."}
		if len(pyFiles) > 0 && ctx.mode != "full" {
			args = append([]string{"check"}, pyFiles...)
		}
		commands = append(commands, validationCommand{name: "ruff check", dir: root, exe: ruff, args: args})
	}
	if python != "" {
		args := []string{"-c", pythonASTValidationScript}
		if len(pyFiles) > 0 && ctx.mode != "full" {
			args = append(args, pyFiles...)
		}
		commands = append(commands, validationCommand{name: "python AST syntax check", dir: root, exe: python, args: args})
	}
	if pytest := tools["pytest"]; pytest != "" && ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "pytest", dir: root, exe: pytest, args: []string{"-q"}})
	}
	return commands
}

func phpAdapterCommands(ctx adapterContext) []validationCommand {
	root, tools := ctx.manager.root, ctx.profile.Environment.Tools
	kinds := kindSet(ctx.profile.Project)
	var commands []validationCommand
	if php := tools["php"]; php != "" {
		for _, file := range filterExt(ctx.changed, ".php") {
			commands = append(commands, validationCommand{name: "php lint " + filepath.ToSlash(file), dir: root, exe: php, args: []string{"-l", file}})
		}
		if kinds["laravel"] && ctx.mode == "full" {
			commands = append(commands, validationCommand{name: "artisan test", dir: root, exe: php, args: []string{"artisan", "test"}})
		}
	}
	if composer := tools["composer"]; composer != "" && ctx.mode == "full" && packageJSONHasScript(filepath.Join(root, "composer.json"), "test") {
		commands = append(commands, validationCommand{name: "composer test", dir: root, exe: composer, args: []string{"run", "test"}})
	}
	return commands
}

func rubyAdapterCommands(ctx adapterContext) []validationCommand {
	root, tools := ctx.manager.root, ctx.profile.Environment.Tools
	ruby := tools["ruby"]
	var commands []validationCommand
	if ruby != "" {
		for _, file := range filterExt(ctx.changed, ".rb") {
			commands = append(commands, validationCommand{name: "ruby syntax " + filepath.ToSlash(file), dir: root, exe: ruby, args: []string{"-c", file}})
		}
	}
	if ctx.mode == "full" {
		if bundle := tools["bundle"]; bundle != "" {
			commands = append(commands, validationCommand{name: "bundle test", dir: root, exe: bundle, args: []string{"exec", "rake", "test"}})
		} else if rake := tools["rake"]; rake != "" {
			commands = append(commands, validationCommand{name: "rake test", dir: root, exe: rake, args: []string{"test"}})
		}
	}
	return commands
}

func dartAdapterCommands(ctx adapterContext) []validationCommand {
	root, tools := ctx.manager.root, ctx.profile.Environment.Tools
	files := filterExt(ctx.changed, ".dart")
	if flutter := tools["flutter"]; flutter != "" {
		var commands []validationCommand
		if ctx.options.ApplyFormat && len(files) > 0 {
			if dart := tools["dart"]; dart != "" {
				commands = append(commands, validationCommand{name: "dart format apply", dir: root, exe: dart, args: append([]string{"format"}, files...)})
			}
		}
		commands = append(commands, validationCommand{name: "flutter analyze", dir: root, exe: flutter, args: []string{"analyze"}})
		if ctx.mode == "full" {
			commands = append(commands, validationCommand{name: "flutter test", dir: root, exe: flutter, args: []string{"test"}})
		}
		return commands
	}
	dart := tools["dart"]
	if dart == "" {
		return nil
	}
	var commands []validationCommand
	if ctx.options.ApplyFormat {
		if len(files) > 0 {
			commands = append(commands, validationCommand{name: "dart format apply", dir: root, exe: dart, args: append([]string{"format"}, files...)})
		}
	} else {
		args := []string{"format", "--output=none", "--set-exit-if-changed"}
		if len(files) > 0 && ctx.mode != "full" {
			args = append(args, files...)
		} else {
			args = append(args, ".")
		}
		commands = append(commands, validationCommand{name: "dart format check", dir: root, exe: dart, args: args})
	}
	commands = append(commands, validationCommand{name: "dart analyze", dir: root, exe: dart, args: []string{"analyze"}})
	if ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "dart test", dir: root, exe: dart, args: []string{"test"}})
	}
	return commands
}

func swiftAdapterCommands(ctx adapterContext) []validationCommand {
	swift := ctx.profile.Environment.Tools["swift"]
	if swift == "" {
		return nil
	}
	command := validationCommand{name: "swift build", dir: ctx.manager.root, exe: swift, args: []string{"build"}}
	if ctx.mode == "full" {
		command = validationCommand{name: "swift test", dir: ctx.manager.root, exe: swift, args: []string{"test"}}
	}
	return []validationCommand{command}
}

func elixirAdapterCommands(ctx adapterContext) []validationCommand {
	mix := ctx.profile.Environment.Tools["mix"]
	if mix == "" {
		return nil
	}
	files := filterExt(ctx.changed, ".ex", ".exs")
	var commands []validationCommand
	if ctx.options.ApplyFormat {
		if len(files) > 0 {
			commands = append(commands, validationCommand{name: "mix format apply", dir: ctx.manager.root, exe: mix, args: append([]string{"format"}, files...)})
		}
	} else {
		commands = append(commands, validationCommand{name: "mix format check", dir: ctx.manager.root, exe: mix, args: []string{"format", "--check-formatted"}})
	}
	commands = append(commands, validationCommand{name: "mix compile", dir: ctx.manager.root, exe: mix, args: []string{"compile", "--warnings-as-errors"}})
	if ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "mix test", dir: ctx.manager.root, exe: mix, args: []string{"test"}})
	}
	return commands
}

func dotnetAdapterCommands(ctx adapterContext) []validationCommand {
	dotnet := ctx.profile.Environment.Tools["dotnet"]
	if dotnet == "" {
		return nil
	}
	var commands []validationCommand
	if ctx.options.ApplyFormat {
		commands = append(commands, validationCommand{name: "dotnet format apply", dir: ctx.manager.root, exe: dotnet, args: []string{"format", "--no-restore"}})
	}
	commands = append(commands, validationCommand{name: "dotnet build", dir: ctx.manager.root, exe: dotnet, args: []string{"build", "--nologo", "--no-restore"}})
	if ctx.mode == "full" {
		commands = append(commands, validationCommand{name: "dotnet test", dir: ctx.manager.root, exe: dotnet, args: []string{"test", "--nologo", "--no-restore"}})
	}
	return commands
}

func godotAdapterCommands(ctx adapterContext) []validationCommand {
	godot := ctx.profile.Environment.Tools["godot4"]
	if godot == "" {
		godot = ctx.profile.Environment.Tools["godot"]
	}
	if godot == "" {
		return nil
	}
	return []validationCommand{{name: "godot headless", dir: ctx.manager.root, exe: godot, args: []string{"--headless", "--path", ctx.manager.root, "--editor", "--quit"}}}
}

func mavenAdapterCommands(ctx adapterContext) []validationCommand {
	exe, prefix := projectWrapper(ctx.manager.root, "mvnw", "mvnw.cmd")
	if exe == "" {
		exe = ctx.profile.Environment.Tools["mvn"]
	}
	if exe == "" {
		return nil
	}
	args := prefixedArgs(prefix, "-q", "-DskipTests", "compile")
	name := "maven compile"
	if ctx.mode == "full" {
		args = prefixedArgs(prefix, "-q", "test")
		name = "maven test"
	}
	return []validationCommand{{name: name, dir: ctx.manager.root, exe: exe, args: args}}
}

func gradleAdapterCommands(ctx adapterContext) []validationCommand {
	root := ctx.manager.root
	exe, prefix := projectWrapper(root, "gradlew", "gradlew.bat")
	if exe == "" {
		exe = ctx.profile.Environment.Tools["gradle"]
	}
	if exe == "" {
		return nil
	}
	task := "classes"
	if ctx.mode == "full" {
		task = "test"
	}
	args := prefixedArgs(prefix, "--no-daemon", task)
	return []validationCommand{{name: "gradle " + task, dir: root, exe: exe, args: args}}
}

func prefixedArgs(prefix []string, args ...string) []string {
	out := append([]string(nil), prefix...)
	return append(out, args...)
}

func projectWrapper(root, unixName, windowsName string) (string, []string) {
	if runtime.GOOS == "windows" {
		candidate := filepath.Join(root, windowsName)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			if shell := os.Getenv("COMSPEC"); shell != "" {
				return shell, []string{"/d", "/s", "/c", candidate}
			}
			if shell, err := exec.LookPath("cmd.exe"); err == nil {
				return shell, []string{"/d", "/s", "/c", candidate}
			}
		}
		return "", nil
	}
	candidate := filepath.Join(root, unixName)
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
		if info.Mode()&0o111 != 0 {
			return candidate, nil
		}
		if shell, err := exec.LookPath("sh"); err == nil {
			return shell, []string{candidate}
		}
	}
	return "", nil
}

func sourceFiles(root string, limit int, extensions ...string) []string {
	allowed := map[string]bool{}
	for _, extension := range extensions {
		allowed[strings.ToLower(extension)] = true
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if path != root && shouldIgnoreDirectory(root, path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !allowed[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && !pathEscapesRoot(rel, nil) {
			files = append(files, rel)
		}
		if limit > 0 && len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

func cmakeAdapterCommands(ctx adapterContext) []validationCommand {
	cmake := ctx.profile.Environment.Tools["cmake"]
	if cmake == "" {
		return nil
	}
	for _, buildDir := range []string{"build", "cmake-build-debug", "cmake-build-release"} {
		if info, err := os.Stat(filepath.Join(ctx.manager.root, buildDir)); err == nil && info.IsDir() {
			return []validationCommand{{name: "cmake build", dir: ctx.manager.root, exe: cmake, args: []string{"--build", buildDir}}}
		}
	}
	return nil
}

func makeAdapterCommands(ctx adapterContext) []validationCommand {
	// CMake owns the build when both CMakeLists.txt and Makefile are present.
	for _, kind := range ctx.profile.Project.Kinds {
		if kind == "cmake" {
			return nil
		}
	}
	makeExe := ctx.profile.Environment.Tools["make"]
	if makeExe == "" {
		return nil
	}
	return []validationCommand{{name: "make", dir: ctx.manager.root, exe: makeExe, args: []string{"-j2"}}}
}
