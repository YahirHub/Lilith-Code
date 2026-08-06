package browser

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type processBrowser struct {
	pid         int
	executable  string
	command     string
	remotePort  int
	userDataDir string
}

func Discover(ctx context.Context) ([]Candidate, error) {
	seen := map[string]Candidate{}
	for _, p := range commonBrowserPaths() {
		if p == "" {
			continue
		}
		if resolved, err := exec.LookPath(p); err == nil {
			p = resolved
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		abs, _ := filepath.Abs(p)
		c := Candidate{
			ID: stableCandidateID(abs), Name: browserName(abs), Executable: abs,
			SupportsVisible: true, Score: executableScore(abs), Reason: "navegador instalado",
		}
		seen[c.ID] = c
	}

	for _, proc := range discoverProcesses(ctx) {
		key := stableCandidateID(proc.executable + "|" + strconv.Itoa(proc.pid))
		c := Candidate{
			ID: key, Name: browserName(proc.executable), Executable: proc.executable,
			Running: true, PID: proc.pid, UserDataDir: proc.userDataDir,
			SupportsVisible: true, Score: 75, Reason: "proceso de navegador en ejecución",
		}
		if proc.remotePort > 0 {
			remote := fmt.Sprintf("http://127.0.0.1:%d", proc.remotePort)
			ws, version := probeRemote(ctx, remote)
			c.RemoteURL, c.WebSocketURL, c.Version = remote, ws, version
			c.SafeToAttach = ws != "" && proc.userDataDir != "" && !IsLikelyDefaultProfile(proc.userDataDir)
			if c.SafeToAttach {
				c.Score += 45
				c.Reason = "proceso CDP con perfil dedicado"
			} else if ws != "" {
				c.Reason = "CDP disponible, pero el perfil no parece dedicado"
			}
		}
		seen[key] = c
	}

	out := make([]Candidate, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return strings.ToLower(out[i].Name+out[i].Executable) < strings.ToLower(out[j].Name+out[j].Executable)
	})
	return out, nil
}

func commonBrowserPaths() []string {
	paths := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave", "vivaldi", "opera", "chrome", "chrome-headless-shell"}
	switch runtime.GOOS {
	case "windows":
		roots := []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")}
		rels := []string{
			`Google\Chrome\Application\chrome.exe`, `Google\Chrome for Testing\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`, `BraveSoftware\Brave-Browser\Application\brave.exe`,
			`Vivaldi\Application\vivaldi.exe`, `Programs\Opera\opera.exe`, `Chromium\Application\chrome.exe`,
		}
		for _, root := range roots {
			for _, rel := range rels {
				if root != "" {
					paths = append(paths, filepath.Join(root, rel))
				}
			}
		}
	case "darwin":
		paths = append(paths,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Vivaldi.app/Contents/MacOS/Vivaldi",
			"/Applications/Opera.app/Contents/MacOS/Opera",
		)
	default:
		paths = append(paths,
			"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser",
			"/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable", "/usr/bin/brave-browser", "/usr/bin/vivaldi-stable",
			"/usr/bin/opera", "/snap/bin/chromium", "/data/data/com.termux/files/usr/bin/chromium-browser",
		)
	}
	return uniqueStrings(paths)
}

func discoverProcesses(ctx context.Context) []processBrowser {
	switch runtime.GOOS {
	case "linux":
		return discoverLinuxProcesses()
	case "windows":
		return discoverWindowsProcesses(ctx)
	default:
		return discoverPSProcesses(ctx)
	}
}

func discoverLinuxProcesses() []processBrowser {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []processBrowser
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		exe, _ := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		cmdBytes, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		cmd := strings.ReplaceAll(string(cmdBytes), "\x00", " ")
		if !looksLikeBrowser(exe + " " + cmd) {
			continue
		}
		port, dataDir := parseProcessFlags(cmd)
		out = append(out, processBrowser{pid: pid, executable: exe, command: cmd, remotePort: port, userDataDir: dataDir})
	}
	return out
}

func discoverPSProcesses(ctx context.Context) []processBrowser {
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,command=")
	data, err := cmd.Output()
	if err != nil {
		return nil
	}
	var out []processBrowser
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		parts := strings.Fields(line)
		if len(parts) < 2 || !looksLikeBrowser(line) {
			continue
		}
		pid, _ := strconv.Atoi(parts[0])
		exe := parts[1]
		port, dataDir := parseProcessFlags(line)
		out = append(out, processBrowser{pid: pid, executable: exe, command: line, remotePort: port, userDataDir: dataDir})
	}
	return out
}

func discoverWindowsProcesses(ctx context.Context) []processBrowser {
	script := "Get-CimInstance Win32_Process | Where-Object {$_.Name -match 'chrome|msedge|brave|chromium|vivaldi|opera'} | ForEach-Object { \"$($_.ProcessId)`t$($_.ExecutablePath)`t$($_.CommandLine)\" }"
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	data, err := cmd.Output()
	if err != nil {
		return nil
	}
	var out []processBrowser
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(parts) < 2 {
			continue
		}
		pid, _ := strconv.Atoi(parts[0])
		command := ""
		if len(parts) == 3 {
			command = parts[2]
		}
		port, dataDir := parseProcessFlags(command)
		out = append(out, processBrowser{pid: pid, executable: parts[1], command: command, remotePort: port, userDataDir: dataDir})
	}
	return out
}

func parseProcessFlags(command string) (int, string) {
	var port int
	var dataDir string
	fields := splitCommandLine(command)
	for i, field := range fields {
		if strings.HasPrefix(field, "--remote-debugging-port=") {
			port, _ = strconv.Atoi(strings.TrimPrefix(field, "--remote-debugging-port="))
		} else if field == "--remote-debugging-port" && i+1 < len(fields) {
			port, _ = strconv.Atoi(fields[i+1])
		} else if strings.HasPrefix(field, "--user-data-dir=") {
			dataDir = strings.Trim(strings.TrimPrefix(field, "--user-data-dir="), `"'`)
		} else if field == "--user-data-dir" && i+1 < len(fields) {
			dataDir = strings.Trim(fields[i+1], `"'`)
		}
	}
	return port, dataDir
}

func splitCommandLine(command string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	for _, r := range command {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote == 0 && (r == ' ' || r == '\t'):
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

func probeRemote(parent context.Context, base string) (string, string) {
	ctx, cancel := context.WithTimeout(parent, 750*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/json/version", nil)
	if err != nil {
		return "", ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	var payload struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if resp.StatusCode/100 != 2 || json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return "", ""
	}
	return payload.WebSocketDebuggerURL, payload.Browser
}

func ResolveRemoteWebSocket(ctx context.Context, remoteURL string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	if strings.HasPrefix(remoteURL, "ws://") || strings.HasPrefix(remoteURL, "wss://") {
		return remoteURL, nil
	}
	ws, _ := probeRemote(ctx, strings.TrimRight(remoteURL, "/"))
	if ws == "" {
		return "", fmt.Errorf("no se encontró un endpoint CDP válido en %s", remoteURL)
	}
	return ws, nil
}

func IsLikelyDefaultProfile(path string) bool {
	clean := strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
	if clean == "" || clean == "." {
		return false
	}
	patterns := []string{
		"google/chrome/user data", "google\\chrome\\user data",
		"chromium", "microsoft/edge/user data", "microsoft\\edge\\user data",
		"bravesoftware/brave-browser/user data", "bravesoftware\\brave-browser\\user data",
		"vivaldi/user data", "vivaldi\\user data", "opera software/opera stable", "opera software\\opera stable",
	}
	for _, pattern := range patterns {
		if strings.HasSuffix(clean, pattern) {
			return true
		}
	}
	return false
}

func browserName(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "msedge") || strings.Contains(strings.ToLower(path), "microsoft edge"):
		return "Microsoft Edge"
	case strings.Contains(base, "brave"):
		return "Brave"
	case strings.Contains(base, "vivaldi"):
		return "Vivaldi"
	case strings.Contains(base, "opera"):
		return "Opera"
	case strings.Contains(base, "headless-shell"):
		return "Chrome Headless Shell"
	case strings.Contains(strings.ToLower(path), "chrome for testing"):
		return "Chrome for Testing"
	case strings.Contains(base, "chromium"):
		return "Chromium"
	default:
		return "Google Chrome"
	}
}

func executableScore(path string) int {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "chrome for testing"):
		return 115
	case strings.Contains(lower, "chromium"):
		return 105
	case strings.Contains(lower, "google-chrome") || strings.HasSuffix(lower, "chrome.exe"):
		return 100
	case strings.Contains(lower, "msedge"):
		return 95
	case strings.Contains(lower, "brave"):
		return 90
	default:
		return 80
	}
}

func looksLikeBrowser(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{"chrome", "chromium", "msedge", "brave", "vivaldi", "opera"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func stableCandidateID(value string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(value))))
	return "browser-" + hex.EncodeToString(sum[:6])
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
