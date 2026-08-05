package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGitZipSchemaRequiresConnectionForRemoteActions(t *testing.T) {
	schema := gitzipSchema()
	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) == 0 {
		t.Fatal("el schema no contiene validación condicional remota")
	}
	condition, ok := allOf[0].(map[string]any)
	if !ok {
		t.Fatalf("condición remota inválida: %#v", allOf[0])
	}
	then, ok := condition["then"].(map[string]any)
	if !ok {
		t.Fatal("falta then remoto")
	}
	required, ok := then["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "connection_id" {
		t.Fatalf("required remoto=%#v", then["required"])
	}
}

func TestCreateLocalGitZipReturnsCodewolfCompatibleSummary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := createLocalGitZip(context.Background(), root, ".", "release.zip", "zip", nil, nil, false, false, -1)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ok", "action", "archive_bytes", "source_bytes", "ignored_entries", "protected_env_excluded", "duration_ms"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("falta campo compatible %s: %s", key, out)
		}
	}
	if result["ok"] != true || result["action"] != "create" {
		t.Fatalf("resumen inesperado: %s", out)
	}
}

func TestGitZipRejectsActionSpecificArgumentsOutsideTheirAction(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{"archive args local", map[string]any{"action": "create", "source_path": ".", "archive_args": []any{"-q"}}},
		{"extract remote create", map[string]any{"action": "remote_create", "source_path": ".", "extract_remote": true}},
		{"extract path local", map[string]any{"action": "create", "source_path": ".", "extract_path": "/tmp/app"}},
		{"cleanup local create", map[string]any{"action": "create", "source_path": ".", "cleanup_local": true}},
		{"cleanup remote create", map[string]any{"action": "remote_create", "source_path": ".", "cleanup_remote_archive": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runGitZip(context.Background(), tt.args, Env{Root: t.TempDir(), ConfigDir: t.TempDir()}); err == nil {
				t.Fatal("se esperaba rechazo de parámetros incompatibles con la acción")
			}
		})
	}
}

func TestRemoteGitZipDoesNotRequestGenericSSHConfirmation(t *testing.T) {
	confirmCalls := 0
	_, err := runGitZip(context.Background(), map[string]any{
		"action":        "remote_extract",
		"source_path":   "/tmp/release.zip",
		"connection_id": "missing",
	}, Env{
		Root:      t.TempDir(),
		ConfigDir: t.TempDir(),
		Confirm: func(context.Context, string, string) (bool, error) {
			confirmCalls++
			return true, nil
		},
	})
	if err == nil {
		t.Fatal("missing connection should still fail")
	}
	if confirmCalls != 0 {
		t.Fatalf("GitZip requested %d generic confirmations", confirmCalls)
	}
}
