package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BrowserProfile describes a browser profile without exposing cookie or account
// data. UserDataDir is the browser data root and ProfileDirectory is the
// Chromium profile subdirectory (for example Default or Profile 1).
type BrowserProfile struct {
	ID               string `json:"id"`
	Browser          string `json:"browser"`
	UserDataDir      string `json:"-"`
	ProfileDirectory string `json:"profile_directory,omitempty"`
	Name             string `json:"name"`
	LastUsed         bool   `json:"last_used,omitempty"`
	CanAttach        bool   `json:"can_attach"`
	remoteURL        string
	Reason           string `json:"reason,omitempty"`
}

type browserProfileRoot struct {
	browser string
	path    string
	single  bool
}

type localStateProfiles struct {
	Profile struct {
		InfoCache map[string]struct {
			Name string `json:"name"`
		} `json:"info_cache"`
		LastUsed string `json:"last_used"`
	} `json:"profile"`
}

func DiscoverProfiles(ctx context.Context) []BrowserProfile {
	var out []BrowserProfile
	seen := map[string]bool{}
	for _, root := range commonProfileRoots() {
		if strings.TrimSpace(root.path) == "" {
			continue
		}
		info, err := os.Stat(root.path)
		if err != nil || !info.IsDir() {
			continue
		}
		remoteURL := liveExistingProfileEndpoint(ctx, root.path)
		if root.single {
			profile := BrowserProfile{
				Browser: root.browser, UserDataDir: root.path, Name: root.browser,
				LastUsed: true, remoteURL: remoteURL, CanAttach: remoteURL != "",
			}
			profile.ID = profileID(profile.UserDataDir, profile.ProfileDirectory)
			if profile.CanAttach {
				profile.Reason = "perfil existente con CDP disponible"
			} else {
				profile.Reason = "perfil existente"
			}
			out = append(out, profile)
			continue
		}

		entries := readProfileEntries(root.path)
		if len(entries) == 0 {
			entries = scanProfileDirectories(root.path)
		}
		for _, profile := range entries {
			key := strings.ToLower(filepath.Clean(root.path) + "|" + profile.ProfileDirectory)
			if seen[key] {
				continue
			}
			seen[key] = true
			profile.Browser = root.browser
			profile.UserDataDir = root.path
			// DevToolsActivePort belongs to the whole User Data root. When it
			// contains multiple profiles, Chrome decides which normal profile is
			// exposed by the live debugging session. Do not pretend an arbitrary
			// sibling profile can be selected through the same browser endpoint.
			profile.CanAttach = remoteURL != "" && profile.LastUsed
			if profile.CanAttach {
				profile.remoteURL = remoteURL
			}
			profile.ID = profileID(profile.UserDataDir, profile.ProfileDirectory)
			if profile.CanAttach {
				profile.Reason = "perfil activo/predeterminado con CDP disponible"
			} else if remoteURL != "" {
				profile.Reason = "CDP está disponible para este navegador, pero este perfil no es el último usado"
			} else {
				profile.Reason = "perfil existente; CDP no está habilitado"
			}
			out = append(out, profile)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CanAttach != out[j].CanAttach {
			return out[i].CanAttach
		}
		if out[i].LastUsed != out[j].LastUsed {
			return out[i].LastUsed
		}
		return strings.ToLower(out[i].Browser+out[i].Name) < strings.ToLower(out[j].Browser+out[j].Name)
	})
	return out
}

func resolveProfileID(ctx context.Context, id string) (BrowserProfile, error) {
	id = strings.TrimSpace(id)
	for _, profile := range DiscoverProfiles(ctx) {
		if profile.ID == id {
			return profile, nil
		}
	}
	return BrowserProfile{}, fmt.Errorf("profile_id no encontrado: %s", id)
}

func readProfileEntries(root string) []BrowserProfile {
	data, err := os.ReadFile(filepath.Join(root, "Local State"))
	if err != nil {
		return nil
	}
	var state localStateProfiles
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	out := make([]BrowserProfile, 0, len(state.Profile.InfoCache))
	for directory, metadata := range state.Profile.InfoCache {
		if !isSafeProfileDirectory(directory) {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, directory)); err != nil || !info.IsDir() {
			continue
		}
		name := strings.TrimSpace(metadata.Name)
		if name == "" {
			name = directory
		}
		out = append(out, BrowserProfile{
			ProfileDirectory: directory,
			Name:             name,
			LastUsed:         directory == state.Profile.LastUsed || (state.Profile.LastUsed == "" && directory == "Default"),
		})
	}
	return out
}

func scanProfileDirectories(root string) []BrowserProfile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []BrowserProfile
	for _, entry := range entries {
		if !entry.IsDir() || !isSafeProfileDirectory(entry.Name()) || !looksLikeChromiumProfileDirectory(entry.Name()) {
			continue
		}
		out = append(out, BrowserProfile{
			ProfileDirectory: entry.Name(),
			Name:             entry.Name(),
			LastUsed:         entry.Name() == "Default",
		})
	}
	return out
}

func looksLikeChromiumProfileDirectory(value string) bool {
	value = strings.TrimSpace(value)
	if value == "Default" || value == "Guest Profile" {
		return true
	}
	if !strings.HasPrefix(value, "Profile ") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(value, "Profile ")))
	return err == nil && n > 0
}

func profileDirectoryIsActive(root, directory string) bool {
	directory = strings.TrimSpace(directory)
	if directory == "" || !isSafeProfileDirectory(directory) {
		return false
	}
	entries := readProfileEntries(root)
	if len(entries) == 0 {
		entries = scanProfileDirectories(root)
	}
	for _, profile := range entries {
		if profile.ProfileDirectory == directory {
			return profile.LastUsed
		}
	}
	return false
}

func isSafeProfileDirectory(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(value)
	return clean == value && !strings.ContainsAny(value, `/\\`)
}

func profileID(userDataDir, profileDirectory string) string {
	return "profile-" + strings.TrimPrefix(stableCandidateID(filepath.Clean(userDataDir)+"|"+profileDirectory), "browser-")
}

func existingProfileEndpoint(userDataDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(userDataDir, "DevToolsActivePort"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return "", errorsProfileActivePort("formato incompleto")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1 || port > 65535 {
		return "", errorsProfileActivePort("puerto inválido")
	}
	path := strings.TrimSpace(lines[1])
	if !strings.HasPrefix(path, "/") {
		return "", errorsProfileActivePort("ruta WebSocket inválida")
	}
	return fmt.Sprintf("ws://127.0.0.1:%d%s", port, path), nil
}

func errorsProfileActivePort(detail string) error {
	return fmt.Errorf("DevToolsActivePort no válido: %s", detail)
}

func liveExistingProfileEndpoint(ctx context.Context, userDataDir string) string {
	ws, err := existingProfileEndpoint(userDataDir)
	if err != nil {
		return ""
	}
	parsed, err := url.Parse(ws)
	if err != nil || parsed.Host == "" || parsed.Scheme != "ws" {
		return ""
	}
	// Chrome 144+ can expose the permission-based remote-debugging endpoint as
	// a direct WebSocket without the legacy /json/version HTTP discovery API.
	// Do not trigger a WebSocket handshake during discovery because that may show
	// the user's approval dialog. A local TCP liveness check is enough here; the
	// actual CDP handshake is performed only by an explicit start action.
	probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", parsed.Host)
	if err != nil {
		return ""
	}
	_ = conn.Close()
	return ws
}

func commonProfileRoots() []browserProfileRoot {
	home, _ := os.UserHomeDir()
	var roots []browserProfileRoot
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		roaming := os.Getenv("APPDATA")
		roots = []browserProfileRoot{
			{"Google Chrome", joinProfileRoot(local, "Google", "Chrome", "User Data"), false},
			{"Chromium", joinProfileRoot(local, "Chromium", "User Data"), false},
			{"Microsoft Edge", joinProfileRoot(local, "Microsoft", "Edge", "User Data"), false},
			{"Brave", joinProfileRoot(local, "BraveSoftware", "Brave-Browser", "User Data"), false},
			{"Vivaldi", joinProfileRoot(local, "Vivaldi", "User Data"), false},
			{"Opera", joinProfileRoot(roaming, "Opera Software", "Opera Stable"), true},
		}
	case "darwin":
		base := joinProfileRoot(home, "Library", "Application Support")
		roots = []browserProfileRoot{
			{"Google Chrome", joinProfileRoot(base, "Google", "Chrome"), false},
			{"Chromium", joinProfileRoot(base, "Chromium"), false},
			{"Microsoft Edge", joinProfileRoot(base, "Microsoft Edge"), false},
			{"Brave", joinProfileRoot(base, "BraveSoftware", "Brave-Browser"), false},
			{"Vivaldi", joinProfileRoot(base, "Vivaldi"), false},
			{"Opera", joinProfileRoot(base, "com.operasoftware.Opera"), true},
		}
	default:
		config := joinProfileRoot(home, ".config")
		roots = []browserProfileRoot{
			{"Google Chrome", joinProfileRoot(config, "google-chrome"), false},
			{"Chromium", joinProfileRoot(config, "chromium"), false},
			{"Microsoft Edge", joinProfileRoot(config, "microsoft-edge"), false},
			{"Brave", joinProfileRoot(config, "BraveSoftware", "Brave-Browser"), false},
			{"Vivaldi", joinProfileRoot(config, "vivaldi"), false},
			{"Opera", joinProfileRoot(config, "opera"), true},
		}
	}
	return roots
}

func joinProfileRoot(base string, elements ...string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	parts := append([]string{base}, elements...)
	return filepath.Join(parts...)
}
