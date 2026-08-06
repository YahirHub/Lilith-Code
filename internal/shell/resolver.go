package shell

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/lilith/li/internal/toolchain"
)

const (
	ShellAuto       = "auto"
	ShellBash       = "bash"
	ShellSh         = "sh"
	ShellPowerShell = "powershell"
	ShellCmd        = "cmd"
	ShellPortable   = "portable"
)

type shellAvailability struct {
	bash       bool
	sh         bool
	powershell bool
	cmd        bool
	portable   bool
}

type resolvedShell struct {
	Kind   string
	Path   string
	Prefix []string
}

var (
	powerShellSyntaxPattern = regexp.MustCompile(`(?i)(\$env:|\$[A-Za-z_][A-Za-z0-9_]*\s*=|\b(?:Get|Set|Test|Join|Write|Select|Where|ForEach)-[A-Za-z][A-Za-z0-9-]*\b|@['"]|\[System\.|\bparam\s*\()`)
	cmdSyntaxPattern        = regexp.MustCompile(`(?i)(%[A-Za-z_][A-Za-z0-9_]*%|(^|[\r\n&|]\s*)(?:set(?:x)?\s+(?:"?[A-Za-z_][A-Za-z0-9_]*=)|if\s+(?:not\s+)?exist\b|for\s+/[A-Za-z]\b|dir(?:\s|$)|copy(?:\s|$)|del(?:\s|$)|type(?:\s|$)|where(?:\s|$)))`)
	posixSyntaxPattern      = regexp.MustCompile(`(?m)(^|[;&|]\s*)[A-Za-z_][A-Za-z0-9_]*=[^\s;&|]+(?:\s+[^\s]|[;\r\n]|$)|\$\(|\$\{|\[\[|/dev/null|(^|[;&|]\s*)(?:export|source|chmod|chown|grep|sed|awk|cat|tee|rm|cp|mv|find|xargs)\b|mkdir\s+-p\b|^#!\s*/(?:usr/bin/env\s+)?(?:ba)?sh\b`)
)

func normalizeRequestedShell(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", ShellAuto:
		return ShellAuto, nil
	case ShellBash:
		return ShellBash, nil
	case ShellSh, "posix":
		return ShellSh, nil
	case ShellPowerShell, "pwsh", "powershell.exe":
		return ShellPowerShell, nil
	case ShellCmd, "cmd.exe":
		return ShellCmd, nil
	case ShellPortable, "embedded", "gosh":
		return ShellPortable, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (use auto, bash, sh, powershell, cmd or portable)", value)
	}
}

func detectCommandShell(command string) string {
	switch {
	case powerShellSyntaxPattern.MatchString(command):
		return ShellPowerShell
	case cmdSyntaxPattern.MatchString(command):
		return ShellCmd
	case posixSyntaxPattern.MatchString(command):
		return ShellBash
	default:
		return ""
	}
}

func chooseShellKind(goos, requested, command string, available shellAvailability) (string, error) {
	requested, err := normalizeRequestedShell(requested)
	if err != nil {
		return "", err
	}
	isAvailable := func(kind string) bool {
		switch kind {
		case ShellBash:
			return available.bash
		case ShellSh:
			return available.sh
		case ShellPowerShell:
			return available.powershell
		case ShellCmd:
			return available.cmd
		case ShellPortable:
			return available.portable
		default:
			return false
		}
	}
	if requested != ShellAuto {
		if !isAvailable(requested) {
			return "", fmt.Errorf("requested shell %s is not available on this host", requested)
		}
		return requested, nil
	}

	detected := detectCommandShell(command)
	if detected != "" {
		if detected == ShellBash {
			if available.bash {
				return ShellBash, nil
			}
			if available.sh {
				return ShellSh, nil
			}
			if available.portable {
				return ShellPortable, nil
			}
		}
		if isAvailable(detected) {
			return detected, nil
		}
		return "", fmt.Errorf("command uses %s syntax but that shell is not available; rewrite the command for the host or set the shell explicitly", detected)
	}

	if goos == "windows" {
		if available.powershell {
			return ShellPowerShell, nil
		}
		if available.cmd {
			return ShellCmd, nil
		}
		if available.bash {
			return ShellBash, nil
		}
		if available.sh {
			return ShellSh, nil
		}
		if available.portable {
			return ShellPortable, nil
		}
		return "", ErrNoShell
	}
	if available.bash {
		return ShellBash, nil
	}
	if available.sh {
		return ShellSh, nil
	}
	if available.portable {
		return ShellPortable, nil
	}
	if available.powershell {
		return ShellPowerShell, nil
	}
	return "", ErrNoShell
}

func currentShellAvailability() shellAvailability {
	_, bash := toolchain.ResolveShell("bash")
	_, sh := toolchain.ResolveShell("sh")
	_, powershell := toolchain.ResolveShell("powershell")
	_, cmd := toolchain.ResolveShell("cmd")
	return shellAvailability{bash: bash, sh: sh, powershell: powershell, cmd: cmd, portable: true}
}

func resolveExecutionShell(requested, command string) (resolvedShell, error) {
	available := currentShellAvailability()
	kind, err := chooseShellKind(runtime.GOOS, requested, command, available)
	if err != nil {
		return resolvedShell{}, err
	}
	if kind == ShellPortable {
		return resolvedShell{Kind: ShellPortable, Path: portableShellPath}, nil
	}
	spec, ok := toolchain.ResolveShell(kind)
	if !ok {
		return resolvedShell{}, fmt.Errorf("shell %s became unavailable while resolving the command", kind)
	}
	return resolvedShell{Kind: kind, Path: spec.Path, Prefix: spec.Prefix}, nil
}

// DefaultKind reports the shell Lilith would use for a syntax-neutral command.
// It does not execute anything and is safe to include in environment profiles.
func DefaultKind() string {
	kind, err := chooseShellKind(runtime.GOOS, ShellAuto, "go version", currentShellAvailability())
	if err != nil {
		return "unavailable"
	}
	return kind
}

// AvailableKinds reports the interpreters Lilith can currently execute.
func AvailableKinds() []string {
	available := currentShellAvailability()
	var out []string
	for _, item := range []struct {
		kind string
		ok   bool
	}{
		{ShellPowerShell, available.powershell},
		{ShellCmd, available.cmd},
		{ShellBash, available.bash},
		{ShellSh, available.sh},
		{ShellPortable, available.portable},
	} {
		if item.ok {
			out = append(out, item.kind)
		}
	}
	return out
}
