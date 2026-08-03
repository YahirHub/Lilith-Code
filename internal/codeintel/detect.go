package codeintel

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	hostshell "github.com/lilith/li/internal/shell"
)

var manifestKinds = map[string]string{
	"go.mod": "go", "go.work": "go-workspace", "Cargo.toml": "rust",
	"package.json": "node", "tsconfig.json": "typescript", "deno.json": "deno", "deno.jsonc": "deno",
	"pyproject.toml": "python", "setup.py": "python", "requirements.txt": "python",
	"composer.json": "php", "artisan": "laravel", "project.godot": "godot",
	"pom.xml": "maven", "build.gradle": "gradle", "build.gradle.kts": "gradle",
	"CMakeLists.txt": "cmake", "Makefile": "make", "Gemfile": "ruby",
	"Package.swift": "swift", "pubspec.yaml": "dart", "mix.exs": "elixir",
}

var extensionLanguages = map[string]string{
	".go": "go", ".rs": "rust", ".py": "python", ".pyi": "python",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascript",
	".ts": "typescript", ".tsx": "typescript", ".vue": "vue", ".svelte": "svelte",
	".java": "java", ".kt": "kotlin", ".kts": "kotlin", ".php": "php",
	".gd": "gdscript", ".tscn": "godot-resource", ".tres": "godot-resource",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp",
	".cs": "csharp", ".rb": "ruby", ".lua": "lua", ".swift": "swift", ".dart": "dart",
	".ex": "elixir", ".exs": "elixir", ".scala": "scala", ".sh": "shell", ".bash": "shell",
	".ps1": "powershell", ".sql": "sql", ".html": "html", ".htm": "html", ".css": "css",
	".scss": "scss", ".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
	".xml": "xml", ".md": "markdown", ".proto": "protobuf",
}

var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".li": true, ".lilith": true,
	"node_modules": true, "vendor": true,
	".next": true, ".nuxt": true, ".cache": true, "__pycache__": true, ".venv": true,
	"venv": true, "coverage": true, ".idea": true, ".vscode": true,
}

var generatedDirNames = map[string]bool{"dist": true, "build": true, "target": true}

func shouldIgnoreDirectory(root, path, name string) bool {
	if ignoredDirs[name] {
		return true
	}
	if !generatedDirNames[name] {
		return false
	}
	// cmd/build is a conventional Go command package, not a generated output
	// directory. Keep the rule structural so equivalent source layouts work.
	parent := strings.ToLower(filepath.Base(filepath.Dir(path)))
	if name == "build" && (parent == "cmd" || parent == "internal" || parent == "pkg" || parent == "src") {
		entries, err := os.ReadDir(path)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && languageForPath(filepath.Join(path, entry.Name())) != "" {
					return false
				}
			}
		}
	}
	return filepath.Clean(path) != filepath.Clean(root)
}

var detectedToolNames = []string{
	"git", "rg", "go", "gofmt", "cargo", "rustfmt", "node", "npm", "pnpm", "yarn", "bun",
	"python", "python3", "pytest", "ruff", "php", "composer", "godot", "godot4", "java", "mvn",
	"gradle", "cmake", "make", "clang", "clang++", "gcc", "g++", "dotnet", "scip",
	"deno", "dart", "flutter", "swift", "ruby", "bundle", "rake", "mix", "elixir",
	"bash", "sh", "pwsh", "powershell", "cmd",
	"gopls", "rust-analyzer", "typescript-language-server", "pyright-langserver", "pylsp",
	"intelephense", "clangd", "jdtls", "kotlin-language-server", "lua-language-server",
	"deno-language-server", "sourcekit-lsp", "ruby-lsp", "solargraph", "elixir-ls",
}

func detectEnvironment() Environment {
	env := Environment{OS: runtime.GOOS, Arch: runtime.GOARCH, Tools: map[string]string{}}
	prefix := os.Getenv("PREFIX")
	home := os.Getenv("HOME")
	env.Termux = runtime.GOOS == "android" || strings.Contains(prefix, "com.termux") || strings.Contains(home, "com.termux")
	env.WSL = os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "" || fileContainsFold("/proc/version", "microsoft")
	env.SSH = os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != ""
	env.Container = os.Getenv("container") != "" || fileExists("/.dockerenv") || fileExists("/run/.containerenv")
	env.Distribution = detectDistribution(env)
	env.Shell = hostshell.DefaultKind()
	env.Shells = hostshell.AvailableKinds()
	env.Path = filepath.SplitList(os.Getenv("PATH"))
	for _, name := range detectedToolNames {
		if p, err := exec.LookPath(name); err == nil {
			env.Tools[name] = p
		}
	}
	for _, name := range []string{"pkg", "apt", "apk", "dnf", "yum", "pacman", "zypper", "brew", "port", "winget", "choco", "scoop"} {
		if _, err := exec.LookPath(name); err == nil {
			env.PackageTools = append(env.PackageTools, name)
		}
	}
	sort.Strings(env.PackageTools)
	return env
}

func detectDistribution(env Environment) string {
	switch {
	case env.Termux:
		return "termux"
	case env.WSL && strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "":
		return strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME"))
	case runtime.GOOS == "windows":
		return "windows"
	case runtime.GOOS == "darwin":
		return "macos"
	}
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
	}
	return firstNonEmpty(values["ID"], values["NAME"], runtime.GOOS)
}

func fileContainsFold(path, needle string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(strings.ToLower(string(data)), strings.ToLower(needle))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func detectProject(root string) Project {
	root, _ = filepath.Abs(root)
	project := Project{Root: root, Languages: map[string]int{}}
	kinds := map[string]bool{}
	manifestRoots := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldIgnoreDirectory(root, path, d.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			if rel != "." && strings.Count(filepath.ToSlash(rel), "/") >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if kind, ok := manifestKinds[d.Name()]; ok {
			project.Manifests = append(project.Manifests, rel)
			kinds[kind] = true
			manifestRoots[filepath.Dir(rel)] = true
		} else {
			switch strings.ToLower(filepath.Ext(d.Name())) {
			case ".csproj", ".fsproj", ".vbproj", ".sln":
				project.Manifests = append(project.Manifests, rel)
				kinds["dotnet"] = true
				manifestRoots[filepath.Dir(rel)] = true
			}
		}
		if lang := languageForPath(path); lang != "" {
			project.Languages[lang]++
		}
		return nil
	})
	for kind := range kinds {
		project.Kinds = append(project.Kinds, kind)
	}
	sort.Strings(project.Kinds)
	sort.Strings(project.Manifests)
	project.Monorepo = len(manifestRoots) > 1
	project.PrimaryLanguage = primaryLanguage(project.Languages)
	project.PackageManager = detectPackageManager(root)
	project.Frameworks = detectFrameworks(root)
	return project
}

func detectFrameworks(root string) []string {
	set := map[string]bool{}
	for _, manifest := range []string{"package.json", "composer.json"} {
		data, err := os.ReadFile(filepath.Join(root, manifest))
		if err != nil {
			continue
		}
		var document map[string]any
		if json.Unmarshal(data, &document) != nil {
			continue
		}
		for _, section := range []string{"dependencies", "devDependencies", "require", "require-dev"} {
			values, _ := document[section].(map[string]any)
			for name := range values {
				switch strings.ToLower(name) {
				case "next":
					set["nextjs"] = true
				case "react", "react-dom":
					set["react"] = true
				case "vue":
					set["vue"] = true
				case "nuxt":
					set["nuxt"] = true
				case "svelte", "@sveltejs/kit":
					set["svelte"] = true
				case "@angular/core":
					set["angular"] = true
				case "vite":
					set["vite"] = true
				case "express":
					set["express"] = true
				case "@nestjs/core":
					set["nestjs"] = true
				case "laravel/framework":
					set["laravel"] = true
				case "symfony/framework-bundle":
					set["symfony"] = true
				}
			}
		}
	}
	for _, file := range []string{"requirements.txt", "pyproject.toml"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			continue
		}
		text := strings.ToLower(string(data))
		for token, framework := range map[string]string{"django": "django", "fastapi": "fastapi", "flask": "flask"} {
			if strings.Contains(text, token) {
				set[framework] = true
			}
		}
	}
	var out []string
	for framework := range set {
		out = append(out, framework)
	}
	sort.Strings(out)
	return out
}

func primaryLanguage(counts map[string]int) string {
	best, max := "", 0
	for name, count := range counts {
		if count > max || count == max && name < best {
			best, max = name, count
		}
	}
	return best
}

func detectPackageManager(root string) string {
	checks := []struct{ file, manager string }{
		{"bun.lockb", "bun"}, {"bun.lock", "bun"}, {"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		{"package-lock.json", "npm"}, {"go.mod", "go"}, {"Cargo.toml", "cargo"}, {"composer.lock", "composer"},
		{"poetry.lock", "poetry"}, {"uv.lock", "uv"}, {"Pipfile", "pipenv"}, {"pyproject.toml", "python"},
		{"pom.xml", "maven"}, {"gradlew", "gradle"}, {"gradlew.bat", "gradle"}, {"build.gradle", "gradle"},
		{"deno.lock", "deno"}, {"deno.json", "deno"}, {"Gemfile.lock", "bundler"}, {"Gemfile", "bundler"},
		{"pubspec.lock", "dart"}, {"pubspec.yaml", "dart"}, {"Package.swift", "swift"}, {"mix.lock", "mix"}, {"mix.exs", "mix"},
	}
	for _, check := range checks {
		if _, err := os.Stat(filepath.Join(root, check.file)); err == nil {
			return check.manager
		}
	}
	return ""
}

func languageForPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "make"
	}
	if lang := extensionLanguages[strings.ToLower(filepath.Ext(path))]; lang != "" {
		return lang
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		switch {
		case strings.HasPrefix(line, "#!") && strings.Contains(line, "python"):
			return "python"
		case strings.HasPrefix(line, "#!") && (strings.Contains(line, "bash") || strings.Contains(line, "/sh")):
			return "shell"
		case strings.HasPrefix(line, "#!") && strings.Contains(line, "node"):
			return "javascript"
		}
	}
	return ""
}
