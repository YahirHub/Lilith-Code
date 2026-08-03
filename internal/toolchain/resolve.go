package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lilith/li/internal/config"
)

// BinDir returns the directory where managed tool binaries live. Por defecto
// es ~/.li/tools/bin, pero puede sobreescribirse con la variable de entorno
// LI_TOOLS_DIR (usada por `make build` para empaquetar todo en `dist/`).
func BinDir() (string, error) {
	if override := os.Getenv("LI_TOOLS_DIR"); override != "" {
		if err := os.MkdirAll(override, config.DirMode); err != nil {
			return "", err
		}
		return override, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "tools", "bin")
	if err := os.MkdirAll(bin, config.DirMode); err != nil {
		return "", err
	}
	return bin, nil
}

// Lookup finds an executable: first inside ~/.li/tools/bin (our managed copy,
// which is deterministic), then on PATH. Returns "" when not found.
func Lookup(name string) string {
	exeName := name
	if runtime.GOOS == "windows" && filepath.Ext(exeName) == "" {
		exeName += ".exe"
	}
	if bin, err := BinDir(); err == nil {
		candidate := filepath.Join(bin, exeName)
		if isExecutable(candidate) {
			return candidate
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// windowsBashCandidates are the usual Git for Windows installation paths.
var windowsBashCandidates = []string{
	`C:\Program Files\Git\bin\bash.exe`,
	`C:\Program Files (x86)\Git\bin\bash.exe`,
	`C:\Program Files\Git\usr\bin\bash.exe`,
}

// ShellSpec describes a concrete command interpreter. It is intentionally
// data-only so callers can choose syntax without spawning a helper process.
type ShellSpec struct {
	Kind   string
	Path   string
	Prefix []string
}

// ResolveShell locates a concrete shell by kind. Supported kinds are bash,
// sh, posix, powershell and cmd. It never downloads a shell.
func ResolveShell(kind string) (ShellSpec, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "bash":
		if p, err := exec.LookPath("bash"); err == nil {
			return ShellSpec{Kind: "bash", Path: p, Prefix: []string{"-c"}}, true
		}
		if runtime.GOOS == "windows" {
			for _, candidate := range windowsBashCandidates {
				if isExecutable(candidate) {
					return ShellSpec{Kind: "bash", Path: candidate, Prefix: []string{"-c"}}, true
				}
			}
		}
	case "sh":
		if p, err := exec.LookPath("sh"); err == nil {
			return ShellSpec{Kind: "sh", Path: p, Prefix: []string{"-c"}}, true
		}
		if runtime.GOOS == "windows" {
			if p := Lookup("busybox"); p != "" {
				return ShellSpec{Kind: "sh", Path: p, Prefix: []string{"sh", "-c"}}, true
			}
		}
	case "posix":
		if spec, ok := ResolveShell("bash"); ok {
			return spec, true
		}
		return ResolveShell("sh")
	case "powershell", "pwsh":
		for _, name := range []string{"pwsh", "powershell.exe", "powershell"} {
			if p, err := exec.LookPath(name); err == nil {
				return ShellSpec{Kind: "powershell", Path: p, Prefix: []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command"}}, true
			}
		}
	case "cmd":
		if runtime.GOOS != "windows" {
			return ShellSpec{}, false
		}
		if comspec := strings.TrimSpace(os.Getenv("COMSPEC")); comspec != "" && isExecutable(comspec) {
			return ShellSpec{Kind: "cmd", Path: comspec, Prefix: []string{"/D", "/S", "/C"}}, true
		}
		for _, name := range []string{"cmd.exe", "cmd"} {
			if p, err := exec.LookPath(name); err == nil {
				return ShellSpec{Kind: "cmd", Path: p, Prefix: []string{"/D", "/S", "/C"}}, true
			}
		}
	}
	return ShellSpec{}, false
}

// ShellCommand returns the executable and the argument prefix used to run a
// command string, e.g. ("/bin/bash", []string{"-c"}).
//
// This legacy helper deliberately resolves a POSIX shell for hooks and build
// compatibility. Interactive agent commands use internal/shell's host-aware
// resolver instead.
func ShellCommand() (string, []string, bool) {
	spec, ok := ResolveShell("posix")
	if !ok {
		return "", nil, false
	}
	return spec.Path, spec.Prefix, true
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// Missing returns the catalog tools that are not available yet.
func Missing() []Tool {
	var out []Tool
	for _, t := range Catalog() {
		name := t.Name
		if name == "busybox" {
			// Sólo hace falta si no hay ningún bash disponible.
			if _, _, ok := ShellCommand(); ok {
				continue
			}
		}
		if Lookup(name) == "" {
			out = append(out, t)
		}
	}
	return out
}
