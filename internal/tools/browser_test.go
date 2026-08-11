package tools

import (
	"path/filepath"
	"testing"
)

func TestBrowserSchemaExposesProfilesExistingModeAndCookieImport(t *testing.T) {
	params := browserParameters()
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("browser properties no tiene el tipo esperado")
	}
	action, ok := properties["action"].(map[string]any)
	if !ok {
		t.Fatal("action schema no tiene el tipo esperado")
	}
	actions, ok := action["enum"].([]string)
	if !ok || !containsBrowserString(actions, "profiles") || !containsBrowserString(actions, "import_cookies") {
		t.Fatalf("acciones nuevas ausentes: %#v", action["enum"])
	}
	mode, ok := properties["profile_mode"].(map[string]any)
	if !ok {
		t.Fatal("profile_mode schema no tiene el tipo esperado")
	}
	modes, ok := mode["enum"].([]string)
	if !ok || !containsBrowserString(modes, "existing") {
		t.Fatalf("profile_mode=existing ausente: %#v", mode["enum"])
	}
	if _, ok := properties["profile_id"]; !ok {
		t.Fatal("profile_id ausente")
	}
	if _, ok := properties["cookie_path"]; !ok {
		t.Fatal("cookie_path ausente")
	}
}

func TestResolveBrowserImportPathUsesProjectForRelativePaths(t *testing.T) {
	root := t.TempDir()
	got := resolveBrowserImportPath(filepath.Join("secrets", "cookies.json"), root)
	want := filepath.Join(root, "secrets", "cookies.json")
	if got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
}

func containsBrowserString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
