package sshremote

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestServerStoreCRUDNeverPersistsLiteralSecrets(t *testing.T) {
	dir := t.TempDir()
	store := NewServerStore(dir)
	profile, err := store.Add(ServerInput{Name: "produccion", Host: "example.com", Username: "deploy", PasswordEnv: "SSH_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Port != 22 {
		t.Fatalf("port=%d", profile.Port)
	}
	updated, err := store.Update(profile.ID, ServerPatch{ServerInput: ServerInput{Host: "new.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Port != 22 || updated.Host != "new.example.com" {
		t.Fatalf("perfil inesperado: %#v", updated)
	}
	renamed, err := store.Rename(profile.ID, "principal", false)
	if err != nil || renamed.Name != "principal" {
		t.Fatalf("rename: %#v %v", renamed, err)
	}
	byRef, err := store.Get("ssh-server://" + profile.ID)
	if err != nil || byRef.ID != profile.ID {
		t.Fatalf("get ref: %#v %v", byRef, err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"password\"", "passphrase\"", "literal-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("el perfil contiene secreto literal %q: %s", forbidden, text)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.Path())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permisos=%o", info.Mode().Perm())
		}
	}
	if _, err = store.Delete(profile.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := store.List(); err != nil || len(list) != 0 {
		t.Fatalf("list final=%#v err=%v", list, err)
	}
}

func TestServerStoreRejectsProtectedEnvAsPrivateKey(t *testing.T) {
	_, err := NewServerStore(t.TempDir()).Add(ServerInput{Host: "host", Username: "user", PrivateKeyPath: ".env"})
	if err == nil || !strings.Contains(err.Error(), ".env") {
		t.Fatalf("error inesperado: %v", err)
	}
}

func TestServerStoreExplicitPatchCanClearOptionalFields(t *testing.T) {
	store := NewServerStore(t.TempDir())
	profile, err := store.Add(ServerInput{
		Name:           "prod",
		Host:           "example.com",
		Username:       "deploy",
		PasswordEnv:    "SSH_PASSWORD",
		PrivateKeyPath: "/tmp/key",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(profile.ID, ServerPatch{Fields: map[string]any{
		"name":             "",
		"password_env":     "",
		"private_key_path": "",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "" || updated.PasswordEnv != "" || updated.PrivateKeyPath != "" {
		t.Fatalf("campos no limpiados: %#v", updated)
	}
}
