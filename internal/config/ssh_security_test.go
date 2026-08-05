package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSSHPolicyDoesNotInterruptCommandsButProtectsCriticalChanges(t *testing.T) {
	s := Defaults()
	if SSHApprovalRequired(s, SSHPermissionCommands) {
		t.Fatal("default policy must not ask before every remote command")
	}
	for _, category := range []SSHPermissionCategory{SSHPermissionFileChanges, SSHPermissionDelete, SSHPermissionCredentials} {
		if !SSHApprovalRequired(s, category) {
			t.Fatalf("default policy must protect %s", category)
		}
	}
}

func TestSSHProjectApprovalsAreScopedAndClearable(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	s := Defaults()
	AddSSHProjectApproval(&s, project, "exec|server-1")
	AddSSHProjectApproval(&s, project, "exec|server-1")
	AddSSHProjectApproval(&s, project, "upload|server-1")
	if !HasSSHProjectApproval(s, project, "EXEC|SERVER-1") {
		t.Fatal("no se encontró el permiso persistido")
	}
	if got := CountSSHProjectApprovals(s, project); got != 2 {
		t.Fatalf("count=%d", got)
	}
	if removed := ClearSSHProjectApprovals(&s, project); removed != 2 {
		t.Fatalf("removed=%d", removed)
	}
	if CountSSHProjectApprovals(s, project) != 0 {
		t.Fatal("los permisos no fueron eliminados")
	}
}

func TestSSHApprovalPresetsAndCustomCategories(t *testing.T) {
	s := Defaults()
	s.SSHRemote.Mode = SSHApprovalCommandsOnly
	if !SSHApprovalRequired(s, SSHPermissionCommands) || SSHApprovalRequired(s, SSHPermissionDelete) {
		t.Fatal("commands-only policy was not applied")
	}
	s.SSHRemote.Mode = SSHApprovalTrustModel
	if SSHApprovalRequired(s, SSHPermissionCommands) || SSHApprovalRequired(s, SSHPermissionDelete) {
		t.Fatal("trust-model policy must not request approval")
	}
	SetSSHCustomPermission(&s.SSHRemote, SSHPermissionRead, true)
	if s.SSHRemote.Mode != SSHApprovalCustom || !SSHApprovalRequired(s, SSHPermissionRead) || SSHApprovalRequired(s, SSHPermissionCommands) {
		t.Fatalf("custom policy=%#v", s.SSHRemote)
	}
	SetSSHCustomPermission(&s.SSHRemote, SSHPermissionVault, true)
	if !SSHApprovalRequired(s, SSHPermissionVault) {
		t.Fatal("custom vault permission was not applied")
	}
}

func TestLoadMigratesLegacySSHSafeModeWithoutCommandPrompts(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"sshSafeMode":true,"protectEnvFiles":true}`)
	if err := os.WriteFile(SettingsPath(dir), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.SSHRemote.Mode != SSHApprovalCriticalOnly {
		t.Fatalf("legacy mode=%q", s.SSHRemote.Mode)
	}
	if SSHApprovalRequired(s, SSHPermissionCommands) {
		t.Fatal("legacy safe mode migration must stop asking before each command")
	}
}
