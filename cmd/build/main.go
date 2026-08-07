// Command build creates stripped Lilith binaries for the supported
// Linux and Windows targets. It also preserves the existing
// toolchain helper used
// by the Makefile:
//
//	go run ./cmd/build                         build public/default binaries
//	go run ./cmd/build build                   build public/default binaries
//	go run ./cmd/build build --distribution company  add the company build tag
//	go run ./cmd/build version     print the release version
//	go run ./cmd/build check       show optional external tool status
//	go run ./cmd/build install     install optional external tools
//	go run ./cmd/build install -f  reinstall optional external tools
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lilith/li/internal/toolchain"
	buildversion "github.com/lilith/li/internal/version"
)

const embeddedGrammarBuildTag = "grammar_set_core"

type target struct {
	GOOS   string
	GOARCH string
	GOARM  string
	Output string
}

var targets = []target{
	{GOOS: "linux", GOARCH: "amd64", Output: "li-linux-amd64"},
	{GOOS: "linux", GOARCH: "arm64", Output: "li-linux-arm64"},
	{GOOS: "linux", GOARCH: "arm", GOARM: "7", Output: "li-linux-armv7"},
	{GOOS: "windows", GOARCH: "amd64", Output: "li-windows-amd64.exe"},
	{GOOS: "windows", GOARCH: "arm64", Output: "li-windows-arm64.exe"},
}

func main() {
	action, args, err := parseAction(os.Args[1:])
	if err != nil {
		fatal(err)
	}

	switch action {
	case "build":
		distribution, err := parseBuildDistribution(args)
		if err != nil {
			fatal(err)
		}
		if err := buildAll(distribution); err != nil {
			fatal(err)
		}
	case "version":
		if len(args) != 0 {
			fatal(fmt.Errorf("version no acepta argumentos: %s", strings.Join(args, " ")))
		}
		version, err := projectVersion()
		if err != nil {
			fatal(err)
		}
		fmt.Println(version)
	case "check", "install":
		if err := runToolchainAction(action, args); err != nil {
			fatal(err)
		}
	default:
		fatal(fmt.Errorf("acción desconocida: %s", action))
	}
}

func parseAction(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "build", nil, nil
	}
	switch args[0] {
	case "build", "version", "check", "install":
		return args[0], args[1:], nil
	default:
		return "", nil, fmt.Errorf("argumento desconocido: %s (usa build, version, check o install)", args[0])
	}
}

var distributionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func parseBuildDistribution(args []string) (string, error) {
	distribution := "default"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--distribution" || arg == "-distribution":
			if i+1 >= len(args) {
				return "", fmt.Errorf("falta valor para %s", arg)
			}
			distribution = args[i+1]
			i++
		case strings.HasPrefix(arg, "--distribution="):
			distribution = strings.TrimPrefix(arg, "--distribution=")
		default:
			return "", fmt.Errorf("argumento desconocido para build: %s", arg)
		}
	}
	distribution = normalizeDistribution(distribution)
	if distribution != "default" && !distributionPattern.MatchString(distribution) {
		return "", fmt.Errorf("distribución inválida %q; usa letras, números, guion o guion bajo", distribution)
	}
	return distribution, nil
}

func normalizeDistribution(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "public" || value == "main" {
		return "default"
	}
	return value
}

func buildTags(distribution string) []string {
	tags := []string{embeddedGrammarBuildTag}
	distribution = normalizeDistribution(distribution)
	if distribution != "default" {
		tags = append(tags, distribution)
	}
	return tags
}

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func projectVersion() (string, error) {
	version := strings.TrimSpace(buildversion.Current)
	version = strings.TrimPrefix(version, "v")
	if !semanticVersionPattern.MatchString(version) {
		return "", fmt.Errorf("versión inválida en internal/version/version.go: %q; usa SemVer, por ejemplo 1.2.3", buildversion.Current)
	}
	return version, nil
}

func buildAll(distribution string) error {
	goExe, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("no se encontró el ejecutable go en PATH: %w", err)
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return fmt.Errorf("crear dist: %w", err)
	}

	version, err := projectVersion()
	if err != nil {
		return err
	}
	commit := gitValue(root, "rev-parse", "--short", "HEAD")
	if commit == "" {
		commit = "none"
	}

	distribution = normalizeDistribution(distribution)
	fmt.Printf("Lilith - build multiplataforma\nProyecto: %s\nVersión: %s (%s)\nDistribución: %s\n\n", root, version, commit, distribution)
	for _, t := range targets {
		if err := buildTarget(goExe, root, dist, version, commit, distribution, t); err != nil {
			return err
		}
	}
	fmt.Printf("\nListo. Binarios li generados en %s\n", dist)
	fmt.Println("Las skills y gramáticas sintácticas principales están embebidas dentro de cada binario estático.")
	return nil
}

func buildTarget(goExe, root, dist, version, commit, distribution string, t target) error {
	out := filepath.Join(dist, t.Output)
	label := t.GOOS + "/" + t.GOARCH
	if t.GOARM != "" {
		label += " v" + t.GOARM
	}
	fmt.Printf("[%s] -> %s\n", label, filepath.Base(out))

	ldflags := fmt.Sprintf("-s -w -X main.version=%s -X main.commit=%s", version, commit)
	args := []string{
		"build",
		"-tags=" + strings.Join(buildTags(distribution), ","),
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=" + ldflags,
		"-o", out,
		"./cmd/li",
	}
	cmd := exec.Command(goExe, args...)
	cmd.Dir = root
	env := sanitizedBuildEnv(os.Environ())
	env = setEnv(env, "CGO_ENABLED", "0")
	env = setEnv(env, "GOOS", t.GOOS)
	env = setEnv(env, "GOARCH", t.GOARCH)
	if t.GOARM != "" {
		env = setEnv(env, "GOARM", t.GOARM)
	} else {
		env = removeEnv(env, "GOARM")
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compilar %s: %w", label, err)
	}
	return nil
}

func runToolchainAction(action string, args []string) error {
	force := false
	targetDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-f", "--force":
			if action != "install" {
				return fmt.Errorf("%s sólo es válido con install", arg)
			}
			force = true
		case "-dir", "--dir":
			if i+1 >= len(args) {
				return fmt.Errorf("falta valor para %s", arg)
			}
			targetDir = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--dir=") {
				targetDir = strings.TrimPrefix(arg, "--dir=")
				continue
			}
			return fmt.Errorf("argumento desconocido para %s: %s", action, arg)
		}
	}
	if targetDir != "" {
		if err := os.Setenv("LI_TOOLS_DIR", targetDir); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runToolchain(ctx, action, force)
}

func runToolchain(ctx context.Context, action string, force bool) error {
	bin, err := toolchain.BinDir()
	if err != nil {
		return err
	}
	fmt.Printf("Plataforma: %s\nDirectorio: %s\n\n", toolchain.Platform(), bin)
	if action == "check" {
		for _, t := range toolchain.Catalog() {
			if p := toolchain.Lookup(t.Name); p != "" {
				fmt.Printf("  ok       %-8s %s\n", t.Name, p)
			} else {
				fmt.Printf("  falta    %-8s %s\n", t.Name, t.Why)
			}
		}
		if sh, prefix, ok := toolchain.ShellCommand(); ok {
			fmt.Printf("\nShell: %s %v\n", sh, prefix)
		} else {
			fmt.Printf("\nShell: no disponible (ejecuta `go run ./cmd/build install`)\n")
		}
		return nil
	}

	if err := toolchain.EnsureAll(ctx, force, func(msg string) {
		fmt.Println(" ", msg)
	}); err != nil {
		return err
	}
	fmt.Println("\nToolchain lista.")
	return nil
}

func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cur := cwd
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no se encontró go.mod desde %s", cwd)
		}
		cur = parent
	}
}

func gitValue(root string, args ...string) string {
	gitExe, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	cmd := exec.Command(gitExe, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), strings.ToUpper(prefix)) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func sanitizedBuildEnv(env []string) []string {
	keys := []string{
		"GOOS", "GOARCH", "GOARM", "GO386", "GOAMD64",
		"GOMIPS", "GOMIPS64", "GOWASM", "CGO_ENABLED",
	}
	out := append([]string{}, env...)
	for _, key := range keys {
		out = removeEnv(out, key)
	}
	return out
}

func removeEnv(env []string, key string) []string {
	prefix := strings.ToUpper(key + "=")
	out := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}
