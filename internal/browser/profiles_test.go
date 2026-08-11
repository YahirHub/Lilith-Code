package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProfileEntriesUsesLocalStateNames(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Default", "Profile 1"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	state := `{"profile":{"last_used":"Profile 1","info_cache":{"Default":{"name":"Personal","user_name":"private@example.com"},"Profile 1":{"name":"Trabajo","user_name":"work@example.com"},"../escape":{"name":"No"}}}}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	profiles := readProfileEntries(root)
	if len(profiles) != 2 {
		t.Fatalf("perfiles=%d want=2: %#v", len(profiles), profiles)
	}
	seen := map[string]BrowserProfile{}
	for _, profile := range profiles {
		seen[profile.ProfileDirectory] = profile
	}
	if seen["Default"].Name != "Personal" || seen["Default"].LastUsed {
		t.Fatalf("perfil Default inesperado: %#v", seen["Default"])
	}
	if seen["Profile 1"].Name != "Trabajo" || !seen["Profile 1"].LastUsed {
		t.Fatalf("perfil Profile 1 inesperado: %#v", seen["Profile 1"])
	}

	encoded, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private@example.com") || strings.Contains(string(encoded), "work@example.com") {
		t.Fatalf("el descubrimiento filtró user_name: %s", encoded)
	}
}

func TestExistingProfileEndpointReadsDevToolsActivePort(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "DevToolsActivePort"), []byte("9227\n/devtools/browser/abc-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := existingProfileEndpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ws://127.0.0.1:9227/devtools/browser/abc-123" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestProfileIDIsStableAndDoesNotExposePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Google", "Chrome", "User Data")
	first := profileID(root, "Profile 2")
	second := profileID(root, "Profile 2")
	if first != second || !strings.HasPrefix(first, "profile-") {
		t.Fatalf("profile id inestable: %q %q", first, second)
	}
	if strings.Contains(strings.ToLower(first), "chrome") || strings.Contains(first, root) {
		t.Fatalf("profile id filtró la ruta: %q", first)
	}
}

func TestIsSafeProfileDirectory(t *testing.T) {
	for _, value := range []string{"Default", "Profile 1", "Guest Profile"} {
		if !isSafeProfileDirectory(value) {
			t.Fatalf("profile_directory válido rechazado: %q", value)
		}
	}
	for _, value := range []string{"", ".", "..", "../Default", `Profile\\One`, "/tmp/Profile"} {
		if isSafeProfileDirectory(value) {
			t.Fatalf("profile_directory inseguro aceptado: %q", value)
		}
	}
}

func TestBrowserProfileJSONHidesInternalRemoteEndpoint(t *testing.T) {
	profile := BrowserProfile{
		ID: "profile-test", Browser: "Google Chrome", Name: "Personal",
		UserDataDir: "/tmp/profile", remoteURL: "ws://127.0.0.1:9222/devtools/browser/private-token",
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "9222") || strings.Contains(string(data), "private-token") || strings.Contains(string(data), "remote_url") || strings.Contains(string(data), "/tmp/profile") || strings.Contains(string(data), "user_data_dir") {
		t.Fatalf("datos internos del perfil aparecieron en JSON: %s", data)
	}
}

func TestJoinProfileRootRejectsEmptyBase(t *testing.T) {
	if got := joinProfileRoot("", "Google", "Chrome"); got != "" {
		t.Fatalf("root relativo inesperado cuando falta base: %q", got)
	}
}

func TestScanProfileDirectoriesIgnoresBrowserInternals(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Default", "Profile 1", "Guest Profile", "Crashpad", "ShaderCache", "component_crx_cache", "Profile nope"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	profiles := scanProfileDirectories(root)
	got := map[string]bool{}
	for _, profile := range profiles {
		got[profile.ProfileDirectory] = true
	}
	for _, want := range []string{"Default", "Profile 1", "Guest Profile"} {
		if !got[want] {
			t.Fatalf("perfil Chromium esperado ausente: %q (%#v)", want, profiles)
		}
	}
	for _, unwanted := range []string{"Crashpad", "ShaderCache", "component_crx_cache", "Profile nope"} {
		if got[unwanted] {
			t.Fatalf("directorio interno confundido con perfil: %q", unwanted)
		}
	}
}

func TestLooksLikeChromiumProfileDirectory(t *testing.T) {
	for _, value := range []string{"Default", "Profile 1", "Profile 27", "Guest Profile"} {
		if !looksLikeChromiumProfileDirectory(value) {
			t.Fatalf("perfil Chromium válido rechazado: %q", value)
		}
	}
	for _, value := range []string{"Crashpad", "ShaderCache", "System Profile", "Profile", "Profile 0", "Profile abc"} {
		if looksLikeChromiumProfileDirectory(value) {
			t.Fatalf("directorio no usuario aceptado como perfil: %q", value)
		}
	}
}

func TestLiveExistingProfileEndpointSupportsDirectWebSocketWithoutHTTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()
	root := t.TempDir()
	port := listener.Addr().(*net.TCPAddr).Port
	data := []byte(fmt.Sprintf("%d\n/devtools/browser/direct-websocket\n", port))
	if err := os.WriteFile(filepath.Join(root, "DevToolsActivePort"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got := liveExistingProfileEndpoint(context.Background(), root)
	want := fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/direct-websocket", port)
	if got != want {
		t.Fatalf("endpoint=%q want=%q", got, want)
	}
}

func TestProfileDirectoryIsActiveUsesLastUsedProfile(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Default", "Profile 2"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	state := `{"profile":{"last_used":"Profile 2","info_cache":{"Default":{"name":"Personal"},"Profile 2":{"name":"Trabajo"}}}}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	if !profileDirectoryIsActive(root, "Profile 2") {
		t.Fatal("el último perfil usado debía considerarse activo")
	}
	if profileDirectoryIsActive(root, "Default") {
		t.Fatal("un perfil hermano no debe considerarse seleccionable por el mismo endpoint CDP")
	}
}
