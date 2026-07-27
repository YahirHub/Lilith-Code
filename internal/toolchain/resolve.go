package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

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

// ShellCommand returns the executable and the argument prefix used to run a
// command string, e.g. ("/bin/bash", []string{"-c"}).
//
// On Windows the order is: bash on PATH, Git for Windows bash, then the
// managed busybox shell. Elsewhere: bash, then sh.
func ShellCommand() (string, []string, bool) {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("bash"); err == nil {
			return p, []string{"-c"}, true
		}
		for _, c := range windowsBashCandidates {
			if isExecutable(c) {
				return c, []string{"-c"}, true
			}
		}
		if p := Lookup("busybox"); p != "" {
			return p, []string{"sh", "-c"}, true
		}
		return "", nil, false
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, []string{"-c"}, true
		}
	}
	return "", nil, false
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
