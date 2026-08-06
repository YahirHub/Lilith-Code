package sshremote

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lilith/li/internal/interaction"
)

// PrivilegeMode controls whether a remote operation may elevate privileges.
// Auto is the safe default: Lilith first uses the SSH account normally and
// elevates only after a write/read permission check fails.
type PrivilegeMode string

const (
	PrivilegeAuto     PrivilegeMode = "auto"
	PrivilegeNever    PrivilegeMode = "never"
	PrivilegeRequired PrivilegeMode = "required"
)

type PrivilegeInfo struct {
	Elevated bool   `json:"elevated"`
	Method   string `json:"method,omitempty"`
}

type privilegeState struct {
	Checked      bool
	Root         bool
	Command      string
	Passwordless bool
	Password     string
	RequirePTY   bool
}

func ParsePrivilegeMode(value string) (PrivilegeMode, error) {
	mode := PrivilegeMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		mode = PrivilegeAuto
	}
	switch mode {
	case PrivilegeAuto, PrivilegeNever, PrivilegeRequired:
		return mode, nil
	default:
		return "", fmt.Errorf("privilege_mode no válido: %s (usa auto, never o required)", value)
	}
}

func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, marker := range []string{
		"permission denied",
		"operation not permitted",
		"access denied",
		"administratively prohibited",
		"ssh_fx_permission_denied",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func noSuchFileResult(result ExecResult) bool {
	text := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	return strings.Contains(text, "no such file or directory") ||
		strings.Contains(text, "cannot stat") ||
		strings.Contains(text, "cannot access")
}

func (c *Connection) execProbe(ctx context.Context, command string) (ExecResult, error) {
	var last ExecResult
	for attempt := 0; attempt < 2; attempt++ {
		result, err := c.Exec(ctx, command, 15*time.Second, false, 0, 0)
		last = result
		if err != nil {
			return result, err
		}
		if !result.TransportRecovered {
			return result, nil
		}
	}
	return last, errors.New("el servidor no confirmó la comprobación después de recuperar el transporte SSH")
}

func (c *Connection) serverPasswordCandidate(ctx context.Context) (string, error) {
	c.mu.Lock()
	opt := c.connectOptions
	manager := c.manager
	c.mu.Unlock()
	if opt.Password != "" {
		return opt.Password, nil
	}
	if opt.PasswordEnv != "" {
		if password := os.Getenv(opt.PasswordEnv); password != "" {
			return password, nil
		}
	}
	if manager != nil && opt.Profile != nil && opt.Profile.PasswordVault {
		secrets, ok, err := manager.vault.GetForConnection(ctx, opt.Profile.ID, DisplayName(*opt.Profile))
		if err != nil {
			return "", err
		}
		if ok && secrets.Password != "" {
			return secrets.Password, nil
		}
	}
	return "", nil
}

func (c *Connection) promptSudoPassword(ctx context.Context) (string, error) {
	if c == nil || c.manager == nil || c.manager.prompt == nil {
		return "", errors.New("sudo requiere contraseña y la entrada secreta local no está disponible")
	}
	snapshot := c.Snapshot()
	message := "La cuenta SSH " + snapshot.Username + "@" + snapshot.Host + " necesita elevar privilegios para acceder a una ruta protegida. Escribe la contraseña sudo de esa cuenta remota. No es la contraseña maestra de la bóveda SSH y no se enviará al modelo ni se guardará en el historial. Se conservará sólo en memoria mientras esta conexión permanezca abierta."
	return c.manager.prompt(ctx, interaction.SecretSudoPassword, "Contraseña sudo del servidor remoto", message, false, 1)
}

func (c *Connection) validateSudoPassword(ctx context.Context, password string, pty bool) (valid bool, needsPTY bool, err error) {
	if strings.ContainsAny(password, "\r\n") {
		return false, false, errors.New("sudo -S no admite contraseñas con saltos de línea")
	}
	result, runErr := c.execWithInput(ctx, "sudo -S -p '' -v", password+"\n", 20*time.Second, pty, 120, 30)
	if runErr != nil {
		return false, false, runErr
	}
	if result.ExitCode == 0 {
		return true, false, nil
	}
	combined := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	if strings.Contains(combined, "terminal is required") || strings.Contains(combined, "must have a tty") || strings.Contains(combined, "no tty present") {
		return false, true, nil
	}
	return false, false, nil
}

func (c *Connection) ensurePrivilegeState(ctx context.Context) (privilegeState, error) {
	c.privilegeMu.Lock()
	defer c.privilegeMu.Unlock()
	if c.privilege.Checked {
		return c.privilege, nil
	}

	uid, err := c.execProbe(ctx, "id -u")
	if err != nil {
		return privilegeState{}, fmt.Errorf("comprobar usuario remoto: %w", err)
	}
	if uid.ExitCode == 0 && strings.TrimSpace(uid.Stdout) == "0" {
		c.privilege = privilegeState{Checked: true, Root: true, Command: "root"}
		return c.privilege, nil
	}

	hasSudo, err := c.execProbe(ctx, "command -v sudo >/dev/null 2>&1")
	if err != nil {
		return privilegeState{}, err
	}
	if hasSudo.ExitCode == 0 {
		passwordless, probeErr := c.execProbe(ctx, "sudo -n true")
		if probeErr != nil {
			return privilegeState{}, probeErr
		}
		if passwordless.ExitCode == 0 {
			c.privilege = privilegeState{Checked: true, Command: "sudo", Passwordless: true}
			return c.privilege, nil
		}

		// Prefer a passwordless doas rule before opening a password popup. Some
		// Alpine/BSD hosts install both commands but intentionally grant elevation
		// only through doas.
		if state, ok, doasErr := c.passwordlessDoasState(ctx); doasErr != nil {
			return privilegeState{}, doasErr
		} else if ok {
			c.privilege = state
			return c.privilege, nil
		}

		candidate, candidateErr := c.serverPasswordCandidate(ctx)
		if candidateErr != nil {
			return privilegeState{}, candidateErr
		}
		if candidate != "" {
			valid, needsPTY, validateErr := c.validateSudoPassword(ctx, candidate, false)
			if validateErr != nil {
				return privilegeState{}, validateErr
			}
			if needsPTY {
				valid, _, validateErr = c.validateSudoPassword(ctx, candidate, true)
				if validateErr != nil {
					return privilegeState{}, validateErr
				}
				if valid {
					c.privilege = privilegeState{Checked: true, Command: "sudo", Password: candidate, RequirePTY: true}
					return c.privilege, nil
				}
			}
			if valid {
				c.privilege = privilegeState{Checked: true, Command: "sudo", Password: candidate}
				return c.privilege, nil
			}
		}

		password, promptErr := c.promptSudoPassword(ctx)
		if promptErr != nil {
			return privilegeState{}, promptErr
		}
		valid, needsPTY, validateErr := c.validateSudoPassword(ctx, password, false)
		if validateErr != nil {
			return privilegeState{}, validateErr
		}
		if needsPTY {
			valid, _, validateErr = c.validateSudoPassword(ctx, password, true)
			if validateErr != nil {
				return privilegeState{}, validateErr
			}
		}
		if !valid {
			return privilegeState{}, errors.New("sudo rechazó la contraseña de la cuenta remota")
		}
		c.privilege = privilegeState{Checked: true, Command: "sudo", Password: password, RequirePTY: needsPTY}
		return c.privilege, nil
	}

	if state, ok, doasErr := c.passwordlessDoasState(ctx); doasErr != nil {
		return privilegeState{}, doasErr
	} else if ok {
		c.privilege = state
		return c.privilege, nil
	}
	return privilegeState{}, errors.New("la cuenta SSH no puede acceder a la ruta protegida y no dispone de root, sudo utilizable ni doas sin contraseña")
}

func (c *Connection) passwordlessDoasState(ctx context.Context) (privilegeState, bool, error) {
	hasDoas, err := c.execProbe(ctx, "command -v doas >/dev/null 2>&1")
	if err != nil {
		return privilegeState{}, false, err
	}
	if hasDoas.ExitCode != 0 {
		return privilegeState{}, false, nil
	}
	passwordless, err := c.execProbe(ctx, "doas -n true")
	if err != nil {
		return privilegeState{}, false, err
	}
	if passwordless.ExitCode != 0 {
		return privilegeState{}, false, nil
	}
	return privilegeState{Checked: true, Command: "doas", Passwordless: true}, true, nil
}

func privilegeMethod(state privilegeState) string {
	if state.Root {
		return "root"
	}
	if state.Command == "doas" {
		return "doas-nopasswd"
	}
	if state.Passwordless {
		return "sudo-nopasswd"
	}
	return "sudo-password"
}

func elevatedCommand(state privilegeState, command string) (wrapped, input string, pty bool, err error) {
	if state.Root {
		return command, "", false, nil
	}
	switch state.Command {
	case "sudo":
		if state.Passwordless {
			return "sudo -n -- sh -c " + quotePOSIX(command), "", state.RequirePTY, nil
		}
		if state.Password == "" {
			return "", "", false, errors.New("sudo requiere una contraseña remota")
		}
		if strings.ContainsAny(state.Password, "\r\n") {
			return "", "", false, errors.New("sudo -S no admite contraseñas con saltos de línea")
		}
		return "sudo -S -p '' -- sh -c " + quotePOSIX(command), state.Password + "\n", state.RequirePTY, nil
	case "doas":
		return "doas -n sh -c " + quotePOSIX(command), "", state.RequirePTY, nil
	default:
		return "", "", false, errors.New("no hay un método de elevación disponible")
	}
}

func (c *Connection) execElevated(ctx context.Context, command string, timeout time.Duration, pty bool, cols, rows int) (ExecResult, PrivilegeInfo, error) {
	state, err := c.ensurePrivilegeState(ctx)
	if err != nil {
		return ExecResult{}, PrivilegeInfo{}, err
	}
	wrapped, input, forcePTY, err := elevatedCommand(state, command)
	if err != nil {
		return ExecResult{}, PrivilegeInfo{}, err
	}
	result, runErr := c.execWithInput(ctx, wrapped, input, timeout, pty || forcePTY, cols, rows)
	return result, PrivilegeInfo{Elevated: !state.Root, Method: privilegeMethod(state)}, runErr
}

// ExecWithPrivilege runs a remote command under the selected privilege policy.
// Auto deliberately behaves like never for arbitrary commands: retrying a
// command after access was denied can duplicate partial side effects. Callers
// such as GitZip must preflight paths and switch to required before execution.
func (c *Connection) ExecWithPrivilege(ctx context.Context, command string, timeout time.Duration, pty bool, cols, rows int, mode PrivilegeMode) (ExecResult, PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return ExecResult{}, PrivilegeInfo{}, err
	}
	if parsed == PrivilegeRequired {
		return c.execElevated(ctx, command, timeout, pty, cols, rows)
	}
	result, runErr := c.Exec(ctx, command, timeout, pty, cols, rows)
	return result, PrivilegeInfo{}, runErr
}

// NeedsWritePrivilege checks the nearest existing destination or parent without
// modifying it. It allows GitZip to choose sudo before a command can partially
// extract or create an archive.
func (c *Connection) NeedsWritePrivilege(ctx context.Context, target string) (bool, error) {
	target = c.resolve(target)
	probe := "p=" + quotePOSIX(target) + "; if [ ! -d \"$p\" ]; then p=$(dirname -- \"$p\"); fi; while [ ! -e \"$p\" ] && [ \"$p\" != / ]; do p=$(dirname -- \"$p\"); done; [ -w \"$p\" ]"
	result, err := c.execProbe(ctx, probe)
	if err != nil {
		return false, err
	}
	if result.ExitCode == 0 {
		return false, nil
	}
	if _, err = c.ensurePrivilegeState(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// NeedsReadPrivilege checks whether the SSH account can traverse and read the
// source before GitZip starts a potentially long archive/extract command.
func (c *Connection) NeedsReadPrivilege(ctx context.Context, target string) (bool, error) {
	target = c.resolve(target)
	probe := "p=" + quotePOSIX(target) + "; [ -r \"$p\" ] && { [ ! -d \"$p\" ] || [ -x \"$p\" ]; }"
	result, err := c.execProbe(ctx, probe)
	if err != nil {
		return false, err
	}
	if result.ExitCode == 0 {
		return false, nil
	}
	if _, err = c.ensurePrivilegeState(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Connection) stagePaths(ctx context.Context, prefix string) []string {
	id := newID(prefix)
	paths := []string{path.Join("/tmp", ".lilith-"+id)}
	if cwd, err := c.Pwd(ctx); err == nil && cwd != "" && cwd != "/tmp" {
		paths = append(paths, path.Join(cwd, ".lilith-"+id))
	}
	return paths
}

func (c *Connection) stageLocalFile(ctx context.Context, local string) (string, os.FileMode, error) {
	localPath, err := expandLocalPath(local)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return "", 0, err
	}
	var last error
	for _, candidate := range c.stagePaths(ctx, "upload") {
		if err = c.Upload(localPath, candidate, true); err == nil {
			return candidate, info.Mode().Perm(), nil
		}
		last = err
		if !IsPermissionDenied(err) {
			break
		}
	}
	return "", 0, fmt.Errorf("preparar archivo temporal remoto: %w", last)
}

func (c *Connection) stageBytes(ctx context.Context, data []byte) (string, error) {
	var last error
	for _, candidate := range c.stagePaths(ctx, "write") {
		if err := c.WriteFile(candidate, string(data), "utf8", true); err == nil {
			return candidate, nil
		} else {
			last = err
			if !IsPermissionDenied(err) {
				break
			}
		}
	}
	return "", fmt.Errorf("preparar contenido temporal remoto: %w", last)
}

func installStageCommand(stage, target string, mode os.FileMode, overwrite bool) string {
	tmpTarget := target + ".lilith-" + newID("install") + ".tmp"
	parent := path.Dir(target)
	parts := []string{
		"set -eu",
		"mkdir -p -- " + quotePOSIX(parent),
		"tmp=" + quotePOSIX(tmpTarget),
		"trap 'rm -f -- \"$tmp\"' EXIT HUP INT TERM",
	}
	if !overwrite {
		parts = append(parts, "if [ -e "+quotePOSIX(target)+" ] || [ -L "+quotePOSIX(target)+" ]; then exit 73; fi")
	}
	parts = append(parts, "if [ -d "+quotePOSIX(target)+" ] && [ ! -L "+quotePOSIX(target)+" ]; then exit 74; fi")
	parts = append(parts,
		"cp -- "+quotePOSIX(stage)+" \"$tmp\"",
		"owner=$(stat -c '%u:%g' -- "+quotePOSIX(target)+" 2>/dev/null || stat -c '%u:%g' -- "+quotePOSIX(parent)+")",
		fmt.Sprintf("perms=$(stat -c '%%a' -- %s 2>/dev/null || printf '%%04o' %d)", quotePOSIX(target), mode.Perm()),
		"chown \"$owner\" -- \"$tmp\"",
		"chmod \"$perms\" -- \"$tmp\"",
		"mv -f -- \"$tmp\" "+quotePOSIX(target),
		"rm -f -- "+quotePOSIX(stage),
		"trap - EXIT HUP INT TERM",
	)
	return strings.Join(parts, "; ")
}

func execPrivilegeFailure(operation string, result ExecResult, err error) error {
	if err != nil {
		return err
	}
	if result.ExitCode == 73 {
		return errors.New("el destino remoto ya existe; usa overwrite=true")
	}
	if result.ExitCode == 74 {
		return errors.New("el destino remoto es un directorio; especifica una ruta de archivo")
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail == "" {
			detail = fmt.Sprintf("exit %d", result.ExitCode)
		}
		return fmt.Errorf("%s: %s", operation, detail)
	}
	return nil
}

func isNonPrivilegeOperationError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"ya existe",
		"already exists",
		"file exists",
		"directory not empty",
		"directorio no está vacío",
		"is a directory",
		"not a directory",
		"invalid argument",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func validateLocalUploadSource(local string) (string, error) {
	localPath, err := expandLocalPath(local)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("la ruta local no es un archivo regular: %s", localPath)
	}
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return localPath, nil
}

func validateLocalDownloadDestination(local string, overwrite bool) (string, error) {
	localPath, err := expandLocalPath(local)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(localPath)
	if statErr == nil {
		if !overwrite {
			return "", errors.New("el archivo local ya existe")
		}
		if info.IsDir() {
			return "", fmt.Errorf("la ruta local es un directorio: %s", localPath)
		}
		file, openErr := os.OpenFile(localPath, os.O_WRONLY, 0)
		if openErr != nil {
			return "", openErr
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", closeErr
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	parent := filepath.Dir(localPath)
	if err = os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	probe, err := os.CreateTemp(parent, ".lilith-download-probe-*")
	if err != nil {
		return "", err
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil {
		return "", closeErr
	}
	if removeErr != nil {
		return "", removeErr
	}
	return localPath, nil
}

func (c *Connection) shouldElevateWriteAfterError(ctx context.Context, target string, operationErr error) bool {
	if operationErr == nil || os.IsNotExist(operationErr) || isNonPrivilegeOperationError(operationErr) {
		return false
	}
	if IsPermissionDenied(operationErr) {
		return true
	}
	needsPrivilege, probeErr := c.NeedsWritePrivilege(ctx, target)
	return probeErr == nil && needsPrivilege
}

func (c *Connection) shouldElevateReadAfterError(ctx context.Context, target string, operationErr error) bool {
	if operationErr == nil || os.IsNotExist(operationErr) || isNonPrivilegeOperationError(operationErr) {
		return false
	}
	if IsPermissionDenied(operationErr) {
		return true
	}
	needsPrivilege, probeErr := c.NeedsReadPrivilege(ctx, target)
	return probeErr == nil && needsPrivilege
}

func parentWriteProbePath(target string) string {
	return path.Join(path.Dir(target), ".lilith-permission-probe")
}

func (c *Connection) UploadWithPrivilege(ctx context.Context, local, remote string, overwrite bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	local, err = validateLocalUploadSource(local)
	if err != nil {
		return PrivilegeInfo{}, err
	}
	remote = c.resolve(remote)
	if parsed != PrivilegeRequired {
		err = c.Upload(local, remote, overwrite)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		// Some SFTP servers collapse EACCES into a generic "failure". Confirm
		// the nearest writable parent before deciding whether to elevate.
		if !c.shouldElevateWriteAfterError(ctx, remote, err) {
			return PrivilegeInfo{}, err
		}
	}
	stage, fileMode, err := c.stageLocalFile(ctx, local)
	if err != nil {
		return PrivilegeInfo{}, err
	}
	defer func() { _ = c.Delete(stage, false) }()
	result, privilege, runErr := c.execElevated(ctx, installStageCommand(stage, remote, fileMode, overwrite), 0, false, 0, 0)
	if err = execPrivilegeFailure("subir archivo con privilegios", result, runErr); err != nil {
		return privilege, err
	}
	return privilege, nil
}

func (c *Connection) WriteFileWithPrivilege(ctx context.Context, target, content, encoding string, overwrite bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if parsed != PrivilegeRequired {
		err = c.WriteFile(target, content, encoding, overwrite)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		if !c.shouldElevateWriteAfterError(ctx, target, err) {
			return PrivilegeInfo{}, err
		}
	}
	data := []byte(content)
	if encoding == "base64" {
		data, err = base64.StdEncoding.DecodeString(content)
		if err != nil {
			return PrivilegeInfo{}, err
		}
	}
	stage, err := c.stageBytes(ctx, data)
	if err != nil {
		return PrivilegeInfo{}, err
	}
	defer func() { _ = c.Delete(stage, false) }()
	result, privilege, runErr := c.execElevated(ctx, installStageCommand(stage, target, 0o644, overwrite), 0, false, 0, 0)
	if err = execPrivilegeFailure("escribir archivo con privilegios", result, runErr); err != nil {
		return privilege, err
	}
	return privilege, nil
}

func (c *Connection) MkdirWithPrivilege(ctx context.Context, target string, recursive bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if parsed != PrivilegeRequired {
		err = c.Mkdir(target, recursive)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		if !c.shouldElevateWriteAfterError(ctx, target, err) {
			return PrivilegeInfo{}, err
		}
	}
	command := "mkdir -- " + quotePOSIX(target)
	if recursive {
		command = "mkdir -p -- " + quotePOSIX(target)
	}
	result, privilege, runErr := c.execElevated(ctx, command, 30*time.Second, false, 0, 0)
	return privilege, execPrivilegeFailure("crear directorio con privilegios", result, runErr)
}

func (c *Connection) RenameWithPrivilege(ctx context.Context, source, destination string, overwrite bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	source, destination = c.resolve(source), c.resolve(destination)
	if parsed != PrivilegeRequired {
		err = c.Rename(source, destination, overwrite)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		if !c.shouldElevateWriteAfterError(ctx, parentWriteProbePath(source), err) &&
			!c.shouldElevateWriteAfterError(ctx, parentWriteProbePath(destination), err) {
			return PrivilegeInfo{}, err
		}
	}
	command := "set -eu; "
	if !overwrite {
		command += "if [ -e " + quotePOSIX(destination) + " ] || [ -L " + quotePOSIX(destination) + " ]; then exit 73; fi; "
	}
	command += "if [ -d " + quotePOSIX(destination) + " ] && [ ! -L " + quotePOSIX(destination) + " ]; then exit 74; fi; "
	command += "mv -f -- " + quotePOSIX(source) + " " + quotePOSIX(destination)
	result, privilege, runErr := c.execElevated(ctx, command, 30*time.Second, false, 0, 0)
	return privilege, execPrivilegeFailure("renombrar ruta con privilegios", result, runErr)
}

func (c *Connection) DeleteWithPrivilege(ctx context.Context, target string, recursive bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if target == "/" {
		return PrivilegeInfo{}, errors.New("se rechazó eliminar la raíz remota /")
	}
	if parsed != PrivilegeRequired {
		err = c.Delete(target, recursive)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		if !c.shouldElevateWriteAfterError(ctx, parentWriteProbePath(target), err) {
			return PrivilegeInfo{}, err
		}
	}
	command := "if [ -d " + quotePOSIX(target) + " ] && [ ! -L " + quotePOSIX(target) + " ]; then rmdir -- " + quotePOSIX(target) + "; else rm -f -- " + quotePOSIX(target) + "; fi"
	if recursive {
		command = "rm -rf -- " + quotePOSIX(target)
	}
	result, privilege, runErr := c.execElevated(ctx, command, 30*time.Second, false, 0, 0)
	return privilege, execPrivilegeFailure("eliminar ruta con privilegios", result, runErr)
}

func (c *Connection) StatWithPrivilege(ctx context.Context, target string, mode PrivilegeMode) (RemoteFileInfo, PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return RemoteFileInfo{}, PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if parsed != PrivilegeRequired {
		info, statErr := c.Stat(target)
		if statErr == nil || parsed == PrivilegeNever {
			return info, PrivilegeInfo{}, statErr
		}
		if !c.shouldElevateReadAfterError(ctx, target, statErr) {
			return info, PrivilegeInfo{}, statErr
		}
	}
	command := "LC_ALL=C stat -c '%s|%F|%a|%Y' -- " + quotePOSIX(target)
	result, privilege, runErr := c.execElevated(ctx, command, 20*time.Second, false, 0, 0)
	if result.ExitCode != 0 && noSuchFileResult(result) {
		return RemoteFileInfo{}, privilege, &os.PathError{Op: "stat", Path: target, Err: os.ErrNotExist}
	}
	if err = execPrivilegeFailure("consultar ruta con privilegios", result, runErr); err != nil {
		return RemoteFileInfo{}, privilege, err
	}
	parts := strings.Split(strings.TrimSpace(result.Stdout), "|")
	if len(parts) < 4 {
		return RemoteFileInfo{}, privilege, errors.New("stat remoto devolvió un formato inesperado")
	}
	size, _ := strconv.ParseInt(parts[0], 10, 64)
	modifiedUnix, _ := strconv.ParseInt(parts[3], 10, 64)
	typeName := "file"
	kind := strings.ToLower(parts[1])
	if strings.Contains(kind, "directory") {
		typeName = "directory"
	} else if strings.Contains(kind, "symbolic link") {
		typeName = "symlink"
	}
	modified := time.Unix(modifiedUnix, 0).UTC().Format(time.RFC3339)
	return RemoteFileInfo{Name: path.Base(target), Path: target, Size: size, Mode: "0" + parts[2], ModifiedAt: modified, Type: typeName}, privilege, nil
}

func (c *Connection) ListWithPrivilege(ctx context.Context, target string, mode PrivilegeMode) ([]RemoteFileInfo, PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return nil, PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if parsed != PrivilegeRequired {
		entries, listErr := c.List(target)
		if listErr == nil || parsed == PrivilegeNever {
			return entries, PrivilegeInfo{}, listErr
		}
		if !c.shouldElevateReadAfterError(ctx, target, listErr) {
			return entries, PrivilegeInfo{}, listErr
		}
	}
	command := "dir=" + quotePOSIX(target) + "; if [ ! -d \"$dir\" ]; then if [ -e \"$dir\" ] || [ -L \"$dir\" ]; then printf 'not a directory: %s\\n' \"$dir\" >&2; exit 20; fi; printf 'no such file or directory: %s\\n' \"$dir\" >&2; exit 44; fi; for f in \"$dir\"/* \"$dir\"/.[!.]* \"$dir\"/..?*; do [ -e \"$f\" ] || [ -L \"$f\" ] || continue; name=${f##*/}; if [ -L \"$f\" ]; then typ=symlink; elif [ -d \"$f\" ]; then typ=directory; else typ=file; fi; size=$(stat -c %s -- \"$f\" 2>/dev/null || printf 0); mode=$(stat -c %a -- \"$f\" 2>/dev/null || printf 000); modified=$(stat -c %Y -- \"$f\" 2>/dev/null || printf 0); printf '%s\\0%s\\0%s\\0%s\\0%s\\0' \"$name\" \"$typ\" \"$size\" \"$mode\" \"$modified\"; done"
	result, privilege, runErr := c.execElevated(ctx, command, 30*time.Second, false, 0, 0)
	if result.ExitCode != 0 && noSuchFileResult(result) {
		return nil, privilege, &os.PathError{Op: "readdir", Path: target, Err: os.ErrNotExist}
	}
	if err = execPrivilegeFailure("listar directorio con privilegios", result, runErr); err != nil {
		return nil, privilege, err
	}
	fields := strings.Split(result.Stdout, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%5 != 0 {
		return nil, privilege, errors.New("el listado remoto privilegiado devolvió un formato inesperado")
	}
	entries := make([]RemoteFileInfo, 0, len(fields)/5)
	for i := 0; i < len(fields); i += 5 {
		size, _ := strconv.ParseInt(fields[i+2], 10, 64)
		modifiedUnix, _ := strconv.ParseInt(fields[i+4], 10, 64)
		entries = append(entries, RemoteFileInfo{
			Name: fields[i], Path: path.Join(target, fields[i]), Size: size,
			Mode: "0" + fields[i+3], ModifiedAt: time.Unix(modifiedUnix, 0).UTC().Format(time.RFC3339), Type: fields[i+1],
		})
	}
	return entries, privilege, nil
}

func (c *Connection) remoteUserIDs(ctx context.Context) (string, string, error) {
	result, err := c.execProbe(ctx, "printf '%s %s' \"$(id -u)\" \"$(id -g)\"")
	if err != nil {
		return "", "", err
	}
	parts := strings.Fields(result.Stdout)
	if result.ExitCode != 0 || len(parts) != 2 {
		return "", "", errors.New("no se pudieron determinar UID/GID de la cuenta SSH")
	}
	return parts[0], parts[1], nil
}

func (c *Connection) stagePrivilegedReadable(ctx context.Context, source string) (string, PrivilegeInfo, error) {
	uid, gid, err := c.remoteUserIDs(ctx)
	if err != nil {
		return "", PrivilegeInfo{}, err
	}
	stage := path.Join("/tmp", ".lilith-"+newID("read"))
	command := "set -eu; cp -- " + quotePOSIX(source) + " " + quotePOSIX(stage) + "; chown " + uid + ":" + gid + " -- " + quotePOSIX(stage) + "; chmod 0600 -- " + quotePOSIX(stage)
	result, privilege, runErr := c.execElevated(ctx, command, 30*time.Second, false, 0, 0)
	if result.ExitCode != 0 && noSuchFileResult(result) {
		return "", privilege, &os.PathError{Op: "read", Path: source, Err: os.ErrNotExist}
	}
	if err = execPrivilegeFailure("preparar lectura privilegiada", result, runErr); err != nil {
		return "", privilege, err
	}
	return stage, privilege, nil
}

func (c *Connection) ReadFileWithPrivilege(ctx context.Context, target, encoding string, max int, mode PrivilegeMode) (content string, size int64, truncated bool, privilege PrivilegeInfo, err error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return "", 0, false, PrivilegeInfo{}, err
	}
	target = c.resolve(target)
	if parsed != PrivilegeRequired {
		content, size, truncated, err = c.ReadFile(target, encoding, max)
		if err == nil || parsed == PrivilegeNever {
			return content, size, truncated, PrivilegeInfo{}, err
		}
		if !c.shouldElevateReadAfterError(ctx, target, err) {
			return content, size, truncated, PrivilegeInfo{}, err
		}
	}
	stage, privilege, err := c.stagePrivilegedReadable(ctx, target)
	if err != nil {
		return "", 0, false, privilege, err
	}
	defer func() { _ = c.Delete(stage, false) }()
	content, size, truncated, err = c.ReadFile(stage, encoding, max)
	return content, size, truncated, privilege, err
}

func (c *Connection) DownloadWithPrivilege(ctx context.Context, remote, local string, overwrite bool, mode PrivilegeMode) (PrivilegeInfo, error) {
	parsed, err := ParsePrivilegeMode(string(mode))
	if err != nil {
		return PrivilegeInfo{}, err
	}
	local, err = validateLocalDownloadDestination(local, overwrite)
	if err != nil {
		return PrivilegeInfo{}, err
	}
	remote = c.resolve(remote)
	if parsed != PrivilegeRequired {
		err = c.Download(remote, local, overwrite)
		if err == nil || parsed == PrivilegeNever {
			return PrivilegeInfo{}, err
		}
		if !c.shouldElevateReadAfterError(ctx, remote, err) {
			return PrivilegeInfo{}, err
		}
	}
	stage, privilege, err := c.stagePrivilegedReadable(ctx, remote)
	if err != nil {
		return privilege, err
	}
	defer func() { _ = c.Delete(stage, false) }()
	return privilege, c.Download(stage, local, overwrite)
}
