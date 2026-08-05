// Package config manages the ~/.li directory and user settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DirName                  = ".li"
	SettingsFile             = "settings.json"
	CurrentOnboardingVersion = 1
	DirMode                  = 0o700
	FileMode                 = 0o600
)

// Settings persisted to ~/.li/settings.json.
type Settings struct {
	OnboardingVersion int    `json:"onboardingVersion"`
	Theme             string `json:"theme,omitempty"`
	// SkillsEnabled activa la carga de Agent Skills desde rutas compatibles de
	// Lilith/Claude/Agent, tanto globales como del proyecto. Off por defecto:
	// sólo se cargan cuando el usuario lo activa desde /config.
	SkillsEnabled bool `json:"skillsEnabled,omitempty"`
	// DisabledSkills conserva excepciones individuales al interruptor global.
	// Los nombres se normalizan a minúsculas y cualquier skill no listada queda
	// habilitada por defecto, incluidas las nuevas skills embebidas de una versión
	// posterior.
	DisabledSkills []string `json:"disabledSkills,omitempty"`
	// ProjectInstructionsEnabled carga LILITH.md/LI.md. Es true por defecto para
	// que un proyecto Lilith pueda llevar instrucciones sin configuración previa.
	ProjectInstructionsEnabled bool `json:"projectInstructionsEnabled"`
	// ClaudeCompatibilityEnabled habilita CLAUDE.md, CLAUDE.local.md,
	// .claude/rules, commands y settings compatibles. Es true por defecto.
	ClaudeCompatibilityEnabled bool `json:"claudeCompatibilityEnabled"`
	// AutoMemoryEnabled carga la memoria persistente curada por Lilith y permite
	// que agentes con memory: user|project|local la actualicen.
	AutoMemoryEnabled bool `json:"autoMemoryEnabled"`
	// HooksEnabled permite ejecutar hooks declarados por el usuario/proyecto.
	// Se puede apagar de emergencia desde /config sin editar settings.json.
	HooksEnabled bool `json:"hooksEnabled"`
	// TrustedProjects authoriza settings/hooks ejecutables del proyecto. Los
	// hooks globales del usuario no requieren esta aprobación explícita.
	TrustedProjects []string `json:"trustedProjects,omitempty"`
	// SSHRemote controla qué categorías de acciones SSH requieren una aprobación
	// humana local. La política predeterminada protege cambios críticos sin
	// interrumpir cada comando remoto.
	SSHRemote SSHRemoteSecurity `json:"sshRemote"`
	// SSHProjectApprovals remembers narrowly-scoped SSH approvals selected from
	// the chat widget. Each rule is tied to one project and one action/server
	// identity; it does not globally disable the SSH security policy.
	SSHProjectApprovals []SSHProjectApproval `json:"sshProjectApprovals,omitempty"`
	// ProtectEnvFiles exige una segunda autorización antes de empaquetar archivos
	// .env reales con GitZip. Las plantillas .env.example/sample siguen permitidas.
	ProtectEnvFiles bool `json:"protectEnvFiles"`
}

// IsSkillEnabled reports whether an individual skill is allowed by settings.
// The global SkillsEnabled switch is intentionally evaluated by the caller so
// this helper can also render and edit per-skill state while the master switch
// is off.
func IsSkillEnabled(s Settings, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, disabled := range s.DisabledSkills {
		if strings.EqualFold(strings.TrimSpace(disabled), name) {
			return false
		}
	}
	return true
}

// SetSkillEnabled updates the normalized disabled-skill set without changing
// the global skills switch. Unknown names are valid so a preference survives a
// temporarily missing project/user skill and applies again when it returns.
func SetSkillEnabled(s *Settings, name string, enabled bool) {
	if s == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	set := make(map[string]struct{}, len(s.DisabledSkills)+1)
	for _, existing := range s.DisabledSkills {
		existing = strings.ToLower(strings.TrimSpace(existing))
		if existing != "" {
			set[existing] = struct{}{}
		}
	}
	if enabled {
		delete(set, name)
	} else {
		set[name] = struct{}{}
	}
	s.DisabledSkills = s.DisabledSkills[:0]
	for existing := range set {
		s.DisabledSkills = append(s.DisabledSkills, existing)
	}
	sort.Strings(s.DisabledSkills)
}

type SSHApprovalMode string

type SSHPermissionCategory string

const (
	SSHApprovalEveryAction  SSHApprovalMode = "every_action"
	SSHApprovalCommandsOnly SSHApprovalMode = "commands_only"
	SSHApprovalCriticalOnly SSHApprovalMode = "critical_only"
	SSHApprovalTrustModel   SSHApprovalMode = "trust_model"
	SSHApprovalCustom       SSHApprovalMode = "custom"

	SSHPermissionConnect     SSHPermissionCategory = "connect"
	SSHPermissionRead        SSHPermissionCategory = "read"
	SSHPermissionCommands    SSHPermissionCategory = "commands"
	SSHPermissionFileChanges SSHPermissionCategory = "file_changes"
	SSHPermissionDelete      SSHPermissionCategory = "delete"
	SSHPermissionCredentials SSHPermissionCategory = "credentials"
	SSHPermissionVault       SSHPermissionCategory = "vault"
)

// SSHRemoteSecurity persists both convenient presets and a custom category
// matrix. Custom values are retained while another preset is active so the
// user can switch away and return without rebuilding the policy.
type SSHRemoteSecurity struct {
	Mode               SSHApprovalMode `json:"mode"`
	ConfirmConnect     bool            `json:"confirmConnect"`
	ConfirmRead        bool            `json:"confirmRead"`
	ConfirmCommands    bool            `json:"confirmCommands"`
	ConfirmFileChanges bool            `json:"confirmFileChanges"`
	ConfirmDelete      bool            `json:"confirmDelete"`
	ConfirmCredentials bool            `json:"confirmCredentials"`
	ConfirmVault       bool            `json:"confirmVault"`
}

type SSHProjectApproval struct {
	Project string `json:"project"`
	Rule    string `json:"rule"`
}

func DefaultSSHRemoteSecurity() SSHRemoteSecurity {
	return SSHRemoteSecurity{
		Mode:               SSHApprovalCriticalOnly,
		ConfirmConnect:     false,
		ConfirmRead:        false,
		ConfirmCommands:    false,
		ConfirmFileChanges: true,
		ConfirmDelete:      true,
		ConfirmCredentials: true,
		ConfirmVault:       false,
	}
}

func NormalizeSSHRemoteSecurity(value SSHRemoteSecurity) SSHRemoteSecurity {
	switch value.Mode {
	case SSHApprovalEveryAction, SSHApprovalCommandsOnly, SSHApprovalCriticalOnly, SSHApprovalTrustModel, SSHApprovalCustom:
	default:
		value.Mode = SSHApprovalCriticalOnly
	}
	return value
}

func SSHApprovalRequired(s Settings, category SSHPermissionCategory) bool {
	security := NormalizeSSHRemoteSecurity(s.SSHRemote)
	switch security.Mode {
	case SSHApprovalEveryAction:
		return category != ""
	case SSHApprovalCommandsOnly:
		return category == SSHPermissionCommands
	case SSHApprovalCriticalOnly:
		return category == SSHPermissionFileChanges || category == SSHPermissionDelete || category == SSHPermissionCredentials
	case SSHApprovalTrustModel:
		return false
	case SSHApprovalCustom:
		switch category {
		case SSHPermissionConnect:
			return security.ConfirmConnect
		case SSHPermissionRead:
			return security.ConfirmRead
		case SSHPermissionCommands:
			return security.ConfirmCommands
		case SSHPermissionFileChanges:
			return security.ConfirmFileChanges
		case SSHPermissionDelete:
			return security.ConfirmDelete
		case SSHPermissionCredentials:
			return security.ConfirmCredentials
		case SSHPermissionVault:
			return security.ConfirmVault
		}
	}
	return false
}

func SetSSHCustomPermission(security *SSHRemoteSecurity, category SSHPermissionCategory, enabled bool) {
	if security == nil {
		return
	}
	security.Mode = SSHApprovalCustom
	switch category {
	case SSHPermissionConnect:
		security.ConfirmConnect = enabled
	case SSHPermissionRead:
		security.ConfirmRead = enabled
	case SSHPermissionCommands:
		security.ConfirmCommands = enabled
	case SSHPermissionFileChanges:
		security.ConfirmFileChanges = enabled
	case SSHPermissionDelete:
		security.ConfirmDelete = enabled
	case SSHPermissionCredentials:
		security.ConfirmCredentials = enabled
	case SSHPermissionVault:
		security.ConfirmVault = enabled
	}
}

func HasSSHProjectApproval(s Settings, project, rule string) bool {
	project = filepath.Clean(strings.TrimSpace(project))
	rule = strings.ToLower(strings.TrimSpace(rule))
	if project == "." || project == "" || rule == "" {
		return false
	}
	for _, approval := range s.SSHProjectApprovals {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(approval.Project)), project) &&
			strings.EqualFold(strings.TrimSpace(approval.Rule), rule) {
			return true
		}
	}
	return false
}

func AddSSHProjectApproval(s *Settings, project, rule string) {
	if s == nil {
		return
	}
	project = filepath.Clean(strings.TrimSpace(project))
	rule = strings.ToLower(strings.TrimSpace(rule))
	if project == "." || project == "" || rule == "" || HasSSHProjectApproval(*s, project, rule) {
		return
	}
	s.SSHProjectApprovals = append(s.SSHProjectApprovals, SSHProjectApproval{Project: project, Rule: rule})
	sort.Slice(s.SSHProjectApprovals, func(i, j int) bool {
		left := strings.ToLower(s.SSHProjectApprovals[i].Project + "\x00" + s.SSHProjectApprovals[i].Rule)
		right := strings.ToLower(s.SSHProjectApprovals[j].Project + "\x00" + s.SSHProjectApprovals[j].Rule)
		return left < right
	})
}

func ClearSSHProjectApprovals(s *Settings, project string) int {
	if s == nil {
		return 0
	}
	project = filepath.Clean(strings.TrimSpace(project))
	kept := s.SSHProjectApprovals[:0]
	removed := 0
	for _, approval := range s.SSHProjectApprovals {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(approval.Project)), project) {
			removed++
			continue
		}
		kept = append(kept, approval)
	}
	s.SSHProjectApprovals = kept
	return removed
}

func CountSSHProjectApprovals(s Settings, project string) int {
	project = filepath.Clean(strings.TrimSpace(project))
	if project == "." || project == "" {
		return 0
	}
	count := 0
	for _, approval := range s.SSHProjectApprovals {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(approval.Project)), project) {
			count++
		}
	}
	return count
}

func Defaults() Settings {
	return Settings{
		ProjectInstructionsEnabled: true,
		ClaudeCompatibilityEnabled: true,
		AutoMemoryEnabled:          true,
		HooksEnabled:               true,
		SSHRemote:                  DefaultSSHRemoteSecurity(),
		ProtectEnvFiles:            true,
	}
}

// Dir returns the config directory path (~/.li), creating it if missing.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	d := filepath.Join(home, DirName)
	if err := os.MkdirAll(d, DirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", d, err)
	}
	// Best-effort permissions fix (no-op on Windows).
	_ = os.Chmod(d, DirMode)
	return d, nil
}

// SettingsPath returns the absolute path to settings.json.
func SettingsPath(dir string) string {
	return filepath.Join(dir, SettingsFile)
}

// Load reads settings.json. Missing file returns zero-value Settings without error.
func Load(dir string) (Settings, error) {
	s := Defaults()
	data, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupted file: return defaults so onboarding can rewrite it.
		return Defaults(), nil
	}
	// Migrate the original all-or-nothing sshSafeMode switch. Its enabled state
	// becomes the less intrusive critical-only preset, which preserves local
	// protection without asking before every remote command.
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if _, hasNew := raw["sshRemote"]; !hasNew {
			if legacyRaw, ok := raw["sshSafeMode"]; ok {
				var legacy bool
				if json.Unmarshal(legacyRaw, &legacy) == nil && !legacy {
					s.SSHRemote.Mode = SSHApprovalTrustModel
				}
			}
		}
	}
	s.SSHRemote = NormalizeSSHRemoteSecurity(s.SSHRemote)
	return s, nil
}

// Save writes settings.json atomically with 0600 permissions.
func Save(dir string, s Settings) error {
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(SettingsPath(dir), data, FileMode)
}

// writeAtomic writes to <path>.tmp then renames.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		// Windows: not fatal.
		_ = err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = dir
	return nil
}

// Complete marks first-run onboarding as done.
func Complete(dir string) error {
	s, _ := Load(dir)
	s.OnboardingVersion = CurrentOnboardingVersion
	return Save(dir, s)
}

// IsProjectTrusted reports whether executable project-level compatibility
// settings may run for this exact project root.
func IsProjectTrusted(s Settings, project string) bool {
	clean := filepath.Clean(project)
	for _, p := range s.TrustedProjects {
		if filepath.Clean(p) == clean {
			return true
		}
	}
	return false
}

func SetProjectTrusted(s *Settings, project string, trusted bool) {
	if s == nil {
		return
	}
	clean := filepath.Clean(project)
	out := make([]string, 0, len(s.TrustedProjects)+1)
	found := false
	for _, p := range s.TrustedProjects {
		if filepath.Clean(p) == clean {
			found = true
			if !trusted {
				continue
			}
		}
		out = append(out, p)
	}
	if trusted && !found {
		out = append(out, clean)
	}
	s.TrustedProjects = out
}
