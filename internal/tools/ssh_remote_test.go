package tools

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/sshremote"
)

func TestSSHRemoteSchemaCoversCodewolfCompatibleActions(t *testing.T) {
	want := []string{"connect", "connect_server", "list_servers", "add_server", "vault_status", "unlock_vault", "exec", "shell_open", "upload", "download", "close_all"}
	seen := map[string]bool{}
	for _, action := range sshActions {
		seen[action] = true
	}
	for _, action := range want {
		if !seen[action] {
			t.Fatalf("falta acción %s", action)
		}
	}
}

func TestServerInputResolvesPrivateKeyFromProjectRoot(t *testing.T) {
	root := t.TempDir()
	input := serverInput(map[string]any{
		"host": "example.com", "username": "deploy", "private_key_path": "keys/id_ed25519",
	}, root)
	want := filepath.Join(root, "keys", "id_ed25519")
	if input.PrivateKeyPath != want {
		t.Fatalf("private_key_path=%q want=%q", input.PrivateKeyPath, want)
	}
}

func TestServerPatchFieldsPreservesExplicitClears(t *testing.T) {
	fields := serverPatchFields(map[string]any{
		"name": "", "password_env": "", "ready_timeout_ms": float64(45000),
	}, t.TempDir())
	if _, ok := fields["name"]; !ok {
		t.Fatal("faltó clear explícito de name")
	}
	if _, ok := fields["password_env"]; !ok {
		t.Fatal("faltó clear explícito de password_env")
	}
	if got := intArgOr(fields, "ready_timeout_ms", 0); got != 45000 {
		t.Fatalf("ready_timeout_ms=%d", got)
	}
}

func TestConnectOptionsResolvesPrivateKeyFromProjectRoot(t *testing.T) {
	root := t.TempDir()
	options := connectOptions(map[string]any{
		"host": "example.com", "username": "deploy", "private_key_path": "keys/id_ed25519",
	}, root)
	want := filepath.Join(root, "keys", "id_ed25519")
	if options.PrivateKeyPath != want {
		t.Fatalf("private_key_path=%q want=%q", options.PrivateKeyPath, want)
	}
}

func TestSSHRemoteSchemaRequiresConnectionAndShellFields(t *testing.T) {
	schema := sshRemoteSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties ausentes")
	}
	if _, ok = properties["shell_id"]; !ok {
		t.Fatal("shell_id no está declarado en el schema")
	}
	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) < 5 {
		t.Fatalf("condiciones insuficientes: %#v", schema["allOf"])
	}
}

func TestCompactProfileExposesCompatibleAuthenticationSummary(t *testing.T) {
	profile := sshremote.ServerProfile{
		ID: "server-1", Host: "example.com", Port: 22, Username: "deploy",
		PasswordVault: true, PrivateKeyPath: "/keys/id_ed25519", Agent: "agent-socket",
	}
	out := compactProfile(profile)
	methods, ok := out["authentication"].([]string)
	if !ok {
		t.Fatalf("authentication=%#v", out["authentication"])
	}
	joined := strings.Join(methods, ",")
	for _, want := range []string{"encrypted_password_vault", "private_key_path", "agent"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("faltó %s en %#v", want, methods)
		}
	}
	if out["password_saved"] != true || out["agent_configured"] != true {
		t.Fatalf("resumen incompatible: %#v", out)
	}
}

func TestTrimSSHOutputPreservesValidUTF8(t *testing.T) {
	got, truncated := trimSSHOutput("línea 🚀 final", 8)
	if !truncated {
		t.Fatal("se esperaba truncamiento")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("UTF-8 inválido: %q", got)
	}
}

func TestEncodedLengthSupportsUTF8AndBase64(t *testing.T) {
	if got, err := encodedLength("á", "utf8"); err != nil || got != 2 {
		t.Fatalf("utf8 bytes=%d err=%v", got, err)
	}
	if got, err := encodedLength("aG9sYQ==", "base64"); err != nil || got != 4 {
		t.Fatalf("base64 bytes=%d err=%v", got, err)
	}
}

func TestSSHPermissionCategoriesRespectConfiguredPolicy(t *testing.T) {
	settings := config.Defaults()
	if category, _ := sshPermissionForAction("exec"); config.SSHApprovalRequired(settings, category) {
		t.Fatal("default policy must not ask before exec")
	}
	if category, _ := sshPermissionForAction("delete"); !config.SSHApprovalRequired(settings, category) {
		t.Fatal("default policy must protect delete")
	}
	settings.SSHRemote.Mode = config.SSHApprovalEveryAction
	if category, _ := sshPermissionForAction("read_file"); !config.SSHApprovalRequired(settings, category) {
		t.Fatal("every-action policy must protect reads")
	}
	settings.SSHRemote.Mode = config.SSHApprovalTrustModel
	if category, _ := sshPermissionForAction("write_file"); config.SSHApprovalRequired(settings, category) {
		t.Fatal("trusted policy must not ask for writes")
	}
}

func TestEverySSHActionHasAnApprovalCategory(t *testing.T) {
	for _, action := range sshActions {
		category, label := sshPermissionForAction(action)
		if category == "" || strings.TrimSpace(label) == "" {
			t.Fatalf("SSH action %q has no approval category", action)
		}
	}
}

func TestSSHExecTimeoutIsUnlimitedWhenOmitted(t *testing.T) {
	if got := secondsArg(map[string]any{}, "timeout_seconds", -1); got != 0 {
		t.Fatalf("default SSH exec timeout=%s; want unlimited", got)
	}
	if got := secondsArg(map[string]any{"timeout_seconds": float64(45)}, "timeout_seconds", -1); got != 45*time.Second {
		t.Fatalf("explicit SSH exec timeout=%s", got)
	}
}

func TestSSHRemoteSchemaExposesPrivilegeModes(t *testing.T) {
	schema := sshRemoteSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties SSH inválidas")
	}
	property, ok := properties["privilege_mode"].(map[string]any)
	if !ok {
		t.Fatal("falta privilege_mode en ssh_remote")
	}
	enum, ok := property["enum"].([]string)
	if !ok || strings.Join(enum, ",") != "auto,never,required" {
		t.Fatalf("enum privilege_mode=%#v", property["enum"])
	}
	if !strings.Contains(property["description"].(string), "exec") {
		t.Fatalf("la descripción no explica la semántica segura de exec: %#v", property)
	}
}
