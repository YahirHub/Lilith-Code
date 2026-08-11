package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/debugger"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	maxConsoleEvents = 300
	maxNetworkEvents = 600
	maxScripts       = 600
)

type Manager struct {
	configDir string
	mu        sync.RWMutex
	sessions  map[string]*Session
}

type Session struct {
	mu sync.RWMutex

	info       SessionInfo
	allocCtx   context.Context
	allocStop  context.CancelFunc
	browserCtx context.Context
	stop       context.CancelFunc
	tabs       map[string]*Tab
	currentTab string
	tempDir    string
	closed     bool
}

type Tab struct {
	ctx    context.Context
	cancel context.CancelFunc
	id     string

	mu                 sync.RWMutex
	console            []ConsoleEvent
	network            []NetworkEvent
	requests           map[string]NetworkEvent
	scripts            map[string]ScriptInfo
	refs               map[string]string
	selectorRefs       map[string]string
	nextRef            int
	lastElements       map[string]Element
	lastSnapshot       *Snapshot
	lastTitle          string
	lastURL            string
	documentGeneration uint64
	lastActivity       time.Time
}

var (
	managersMu sync.Mutex
	managers   = map[string]*Manager{}
)

func GetManager(configDir string) *Manager {
	key := filepath.Clean(configDir)
	managersMu.Lock()
	defer managersMu.Unlock()
	if manager := managers[key]; manager != nil {
		return manager
	}
	manager := &Manager{configDir: key, sessions: map[string]*Session{}}
	managers[key] = manager
	return manager
}

func enableDebugger() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := debugger.Enable().Do(ctx)
		return err
	})
}

func ShutdownAll() {
	managersMu.Lock()
	all := make([]*Manager, 0, len(managers))
	for _, manager := range managers {
		all = append(all, manager)
	}
	managers = map[string]*Manager{}
	managersMu.Unlock()
	for _, manager := range all {
		manager.CloseAll()
	}
}

func (m *Manager) Start(ctx context.Context, opts StartOptions) (SessionInfo, error) {
	if strings.TrimSpace(m.configDir) == "" {
		return SessionInfo{}, errors.New("directorio de configuración de Lilith no disponible")
	}
	if opts.ProfileMode == "" {
		opts.ProfileMode = ProfileTemporary
	}
	if opts.StartURL == "" {
		opts.StartURL = "about:blank"
	}
	id := strings.TrimSpace(opts.SessionID)
	if id == "" {
		id = newID("browser")
	} else if err := validateSessionID(id); err != nil {
		return SessionInfo{}, err
	}
	m.mu.RLock()
	duplicate := m.sessions[id] != nil
	m.mu.RUnlock()
	if duplicate {
		return SessionInfo{}, fmt.Errorf("ya existe una sesión de navegador con session_id %q", id)
	}

	if strings.TrimSpace(opts.ProfileID) != "" {
		profile, err := resolveProfileID(ctx, opts.ProfileID)
		if err != nil {
			return SessionInfo{}, err
		}
		if !profile.CanAttach || profile.remoteURL == "" {
			return SessionInfo{}, errors.New("el profile_id seleccionado no corresponde al perfil activo que Chrome expone por Remote Debugging; vuelve a ejecutar action=profiles y elige uno con can_attach=true")
		}
		opts.ProfileMode = ProfileExisting
		opts.UserDataDir = profile.UserDataDir
		opts.ProfileDirectory = profile.ProfileDirectory
		// profile_id is authoritative: never let an unrelated explicit/default
		// remote URL make the session appear attached to a different profile.
		opts.RemoteURL = profile.remoteURL
	}
	if opts.ProfileMode == ProfileExisting && strings.TrimSpace(opts.ProfileID) == "" && strings.TrimSpace(opts.UserDataDir) != "" {
		if strings.TrimSpace(opts.ProfileDirectory) != "" && !profileDirectoryIsActive(opts.UserDataDir, opts.ProfileDirectory) {
			return SessionInfo{}, errors.New("profile_directory no corresponde al último perfil usado de ese User Data; usa action=profiles y selecciona un profile_id con can_attach=true")
		}
		if strings.TrimSpace(opts.RemoteURL) == "" {
			if endpoint := liveExistingProfileEndpoint(ctx, opts.UserDataDir); endpoint != "" {
				opts.RemoteURL = endpoint
			}
		}
	}

	if strings.TrimSpace(opts.CandidateID) != "" && strings.TrimSpace(opts.Executable) == "" && strings.TrimSpace(opts.RemoteURL) == "" {
		executable, remoteURL, err := resolveCandidate(ctx, opts.CandidateID)
		if err != nil {
			return SessionInfo{}, err
		}
		opts.Executable = executable
		opts.RemoteURL = remoteURL
	}
	if opts.ProfileMode == ProfileExisting && strings.TrimSpace(opts.RemoteURL) == "" {
		return SessionInfo{}, errors.New("el perfil existente seleccionado no expone una sesión CDP reutilizable; habilita Remote Debugging en ese navegador o importa un JSON de cookies en un perfil persistente de Lilith")
	}

	var allocCtx context.Context
	var allocStop context.CancelFunc
	var executable, remoteURL, userDataDir, tempDir string
	remoteAttached := false

	if strings.TrimSpace(opts.RemoteURL) != "" {
		userDataDir = strings.TrimSpace(opts.UserDataDir)
		wsURL, err := ResolveRemoteWebSocket(ctx, opts.RemoteURL)
		if err != nil {
			return SessionInfo{}, err
		}
		remoteOptions := []chromedp.RemoteAllocatorOption{}
		if strings.Contains(wsURL, "/devtools/browser/") {
			// ResolveRemoteWebSocket already returned the complete browser
			// WebSocket URL. Avoid legacy /json/version URL rewriting so the
			// permission-based Chrome 144+ endpoint can be used directly.
			remoteOptions = append(remoteOptions, chromedp.NoModifyURL)
		}
		allocCtx, allocStop = chromedp.NewRemoteAllocator(context.Background(), wsURL, remoteOptions...)
		remoteURL = opts.RemoteURL
		remoteAttached = true
	} else {
		executable = strings.TrimSpace(opts.Executable)
		if executable == "" {
			candidates, err := Discover(ctx)
			if err != nil {
				return SessionInfo{}, err
			}
			for _, candidate := range candidates {
				if candidate.Executable != "" {
					executable = candidate.Executable
					break
				}
			}
		}
		if executable == "" {
			return SessionInfo{}, errors.New("no se encontró Chrome, Chromium, Edge, Brave u otro navegador compatible")
		}
		if info, err := os.Stat(executable); err != nil || info.IsDir() {
			return SessionInfo{}, fmt.Errorf("ejecutable de navegador no válido: %s", executable)
		}
		var err error
		userDataDir, tempDir, err = m.prepareProfile(opts)
		if err != nil {
			return SessionInfo{}, err
		}
		allocatorOptions := execAllocatorOptions(executable, userDataDir, opts.ProfileDirectory, opts.Headless)
		allocCtx, allocStop = chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	}

	browserCtx, stop := chromedp.NewContext(allocCtx)
	// The first chromedp.Run allocates both the browser and the initial target.
	// It must run on the long-lived context itself: cancelling a temporary child
	// context used for the first Run also tears down the target executor and makes
	// every later operation fail with context canceled.
	if err := runInitial(browserCtx, stop, 45*time.Second); err != nil {
		stop()
		allocStop()
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
		return SessionInfo{}, fmt.Errorf("iniciar navegador CDP: %w", err)
	}

	name := browserName(executable)
	if remoteAttached {
		name = "Navegador CDP remoto"
	}
	now := time.Now()
	infoRemoteURL, infoUserDataDir := remoteURL, userDataDir
	if opts.ProfileMode == ProfileExisting {
		// An existing personal profile may contain the OS username in its path
		// and its WebSocket endpoint acts as a local control capability. Keep
		// both inside the runtime instead of returning them to the model.
		infoRemoteURL = ""
		infoUserDataDir = ""
	}
	s := &Session{
		info: SessionInfo{
			ID: id, Browser: name, Executable: executable, RemoteURL: infoRemoteURL,
			Headless: opts.Headless, ProfileMode: opts.ProfileMode, ProfileID: opts.ProfileID,
			ProfileDirectory: opts.ProfileDirectory, UserDataDir: infoUserDataDir,
			StartedAt: now, LastActivity: now, Attached: true, RemoteAttached: remoteAttached, TemporaryData: tempDir != "",
		},
		allocCtx: allocCtx, allocStop: allocStop, browserCtx: browserCtx, stop: stop,
		tabs: map[string]*Tab{}, tempDir: tempDir,
	}
	if err := s.adoptInitialTab(); err != nil {
		s.close()
		return SessionInfo{}, err
	}
	if opts.StartURL != "" && opts.StartURL != "about:blank" {
		if err := s.Navigate(ctx, opts.StartURL); err != nil {
			s.close()
			return SessionInfo{}, err
		}
	}
	m.mu.Lock()
	if m.sessions[id] != nil {
		m.mu.Unlock()
		s.close()
		return SessionInfo{}, fmt.Errorf("ya existe una sesión de navegador con session_id %q", id)
	}
	m.sessions[id] = s
	m.mu.Unlock()
	return s.Info(), nil
}

// runInitial executes the first chromedp.Run for a browser or target context.
// Chromedp documents that a timeout context must not wrap the first Run because
// cancelling that child also stops the allocated browser/target. We keep the
// persistent context and enforce the deadline by cancelling it only when startup
// truly exceeds the limit.
func runInitial(ctx context.Context, cancel context.CancelFunc, timeout time.Duration, actions ...chromedp.Action) error {
	return runInitialWith(ctx, cancel, timeout, chromedp.Run, actions...)
}

type initialRunner func(context.Context, ...chromedp.Action) error

func runInitialWith(ctx context.Context, cancel context.CancelFunc, timeout time.Duration, run initialRunner, actions ...chromedp.Action) error {
	if timeout <= 0 {
		return run(ctx, actions...)
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		cancel()
	})
	err := run(ctx, actions...)
	timer.Stop()
	if timedOut.Load() && (err == nil || errors.Is(err, context.Canceled)) {
		return context.DeadlineExceeded
	}
	return err
}

// operationContext derives an action-scoped context from the persistent
// Chromedp context. It observes cancellation from the tool request without ever
// cancelling the browser or target context that owns the CDP executor.
func operationContext(request, persistent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if persistent == nil {
		persistent = context.Background()
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(persistent, timeout)
	} else {
		ctx, cancel = context.WithCancel(persistent)
	}
	if request == nil || request == persistent {
		return ctx, cancel
	}
	stopRequest := context.AfterFunc(request, cancel)
	return ctx, func() {
		stopRequest()
		cancel()
	}
}

func runBrowserCommand(request, persistent context.Context, timeout time.Duration, command func(context.Context) error) error {
	opCtx, cancel := operationContext(request, persistent, timeout)
	defer cancel()
	state := chromedp.FromContext(persistent)
	if state == nil || state.Browser == nil {
		return errors.New("conexión CDP del navegador no inicializada")
	}
	return command(cdp.WithExecutor(opCtx, state.Browser))
}

func runTargetCommand(request, persistent context.Context, timeout time.Duration, command func(context.Context) error) error {
	opCtx, cancel := operationContext(request, persistent, timeout)
	defer cancel()
	state := chromedp.FromContext(persistent)
	if state == nil || state.Target == nil {
		return errors.New("target CDP no inicializado")
	}
	return command(cdp.WithExecutor(opCtx, state.Target))
}

func execAllocatorOptions(executable, userDataDir, profileDirectory string, headless bool) []chromedp.ExecAllocatorOption {
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.UserDataDir(userDataDir),
		// DefaultExecAllocatorOptions enables these three flags. Explicitly
		// overwrite all of them so visible sessions are actually visible.
		chromedp.Flag("headless", headless),
		chromedp.Flag("hide-scrollbars", headless),
		chromedp.Flag("mute-audio", headless),
		// Login flows and application debugging need normal background network
		// activity. A false boolean removes the default Chromium switch.
		chromedp.Flag("disable-background-networking", false),
		chromedp.WSURLReadTimeout(45*time.Second),
	)
	if strings.TrimSpace(profileDirectory) != "" {
		options = append(options, chromedp.Flag("profile-directory", profileDirectory))
	}
	// Do not set remote-debugging-port here. Chromedp automatically appends
	// --remote-debugging-port=0 when the option is absent. Passing the integer
	// 0 to chromedp.Flag is invalid because allocator flags only accept string
	// or bool values and causes "invalid exec pool flag" before Chrome starts.
	return options
}

func (m *Manager) prepareProfile(opts StartOptions) (string, string, error) {
	base := filepath.Join(m.configDir, "browser")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", "", err
	}
	switch opts.ProfileMode {
	case ProfileTemporary:
		dir, err := os.MkdirTemp(filepath.Join(base), "temporary-profile-")
		return dir, dir, err
	case ProfilePersistent:
		name := sanitizeProfileName(opts.ProfileName)
		if name == "" {
			name = "default"
		}
		dir := filepath.Join(base, "profiles", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		return dir, "", nil
	case ProfileExisting:
		return "", "", errors.New("profile_mode=existing sólo puede adjuntarse a una sesión CDP existente")
	case ProfileCustom:
		dir := filepath.Clean(strings.TrimSpace(opts.UserDataDir))
		if dir == "" || dir == "." {
			return "", "", errors.New("profile_mode=custom requiere user_data_dir")
		}
		if IsLikelyDefaultProfile(dir) {
			return "", "", errors.New("se rechazó el perfil personal predeterminado; usa un directorio dedicado para Lilith")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		return dir, "", nil
	default:
		return "", "", fmt.Errorf("profile_mode desconocido: %s", opts.ProfileMode)
	}
}

func resolveCandidate(ctx context.Context, candidateID string) (string, string, error) {
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" {
		return "", "", errors.New("candidate_id es obligatorio")
	}
	candidates, err := Discover(ctx)
	if err != nil {
		return "", "", err
	}
	for _, candidate := range candidates {
		if candidate.ID != candidateID {
			continue
		}
		if candidate.SafeToAttach && candidate.RemoteURL != "" {
			return "", candidate.RemoteURL, nil
		}
		if candidate.Executable != "" {
			return candidate.Executable, "", nil
		}
		return "", "", fmt.Errorf("candidate_id %s no tiene ejecutable ni endpoint CDP utilizable", candidateID)
	}
	return "", "", fmt.Errorf("candidate_id no encontrado: %s", candidateID)
}

func (m *Manager) SetDefault(ctx context.Context, candidateID, executable, remoteURL string, headless bool, mode ProfileMode, profileName, profileID, profileDirectory, userDataDir string) (Config, error) {
	cfg, err := LoadConfig(m.configDir)
	if err != nil {
		return Config{}, err
	}
	candidateID = strings.TrimSpace(candidateID)
	if candidateID != "" && executable == "" && remoteURL == "" {
		executable, remoteURL, err = resolveCandidate(ctx, candidateID)
		if err != nil {
			return Config{}, err
		}
	}
	profileID = strings.TrimSpace(profileID)
	if profileID != "" {
		profile, profileErr := resolveProfileID(ctx, profileID)
		if profileErr != nil {
			return Config{}, profileErr
		}
		mode = ProfileExisting
		profileDirectory = profile.ProfileDirectory
		// profile_id is sufficient to resolve the source on start. Do not
		// persist the personal User Data path in browser.json.
		userDataDir = ""
	}
	if mode == "" {
		mode = cfg.ProfileMode
		if mode == "" {
			mode = ProfileTemporary
		}
	}
	cfg.DefaultCandidateID = candidateID
	cfg.Executable = strings.TrimSpace(executable)
	cfg.RemoteURL = strings.TrimSpace(remoteURL)
	cfg.Headless = headless
	cfg.ProfileMode = mode
	cfg.ProfileName = profileName
	cfg.ProfileID = profileID
	cfg.ProfileDirectory = profileDirectory
	cfg.UserDataDir = userDataDir
	if err := SaveConfig(m.configDir, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (m *Manager) StartDefault(ctx context.Context, overrides StartOptions) (SessionInfo, error) {
	cfg, err := LoadConfig(m.configDir)
	if err != nil {
		return SessionInfo{}, err
	}
	if overrides.CandidateID == "" && overrides.Executable == "" && overrides.RemoteURL == "" {
		overrides.CandidateID = cfg.DefaultCandidateID
		overrides.Executable = cfg.Executable
		overrides.RemoteURL = cfg.RemoteURL
	}
	if overrides.ProfileMode == "" {
		overrides.ProfileMode = cfg.ProfileMode
	}
	if overrides.ProfileName == "" {
		overrides.ProfileName = cfg.ProfileName
	}
	if overrides.ProfileID == "" {
		overrides.ProfileID = cfg.ProfileID
	}
	if overrides.ProfileDirectory == "" {
		overrides.ProfileDirectory = cfg.ProfileDirectory
	}
	if overrides.UserDataDir == "" {
		overrides.UserDataDir = cfg.UserDataDir
	}
	return m.Start(ctx, overrides)
}

func (m *Manager) Session(id string) (*Session, error) {
	m.mu.RLock()
	s := m.sessions[strings.TrimSpace(id)]
	m.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("sesión de navegador no encontrada: %s", id)
	}
	return s, nil
}

func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("sesión de navegador no encontrada: %s", id)
	}
	s.close()
	return nil
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}

func (s *Session) adoptInitialTab() error {
	cdpCtx := chromedp.FromContext(s.browserCtx)
	if cdpCtx == nil || cdpCtx.Target == nil {
		return errors.New("Chromedp no devolvió una pestaña inicial")
	}
	id := string(cdpCtx.Target.TargetID)
	// browserCtx is the long-lived context of the initial target. Its cancel
	// function belongs to the whole session, not to an individual tab.
	tab := s.newTab(s.browserCtx, nil, id)
	s.tabs[id] = tab
	s.currentTab = id
	s.info.CurrentTabID = id
	s.info.TabCount = 1
	if err := runWithTimeout(tab.ctx, 20*time.Second, network.Enable(), cdpruntime.Enable(), enableDebugger()); err != nil {
		return fmt.Errorf("habilitar depuración del navegador: %w", err)
	}
	return nil
}

func (s *Session) newTab(ctx context.Context, cancel context.CancelFunc, id string) *Tab {
	t := &Tab{ctx: ctx, cancel: cancel, id: id, requests: map[string]NetworkEvent{}, scripts: map[string]ScriptInfo{}, refs: map[string]string{}, selectorRefs: map[string]string{}, nextRef: 1, lastElements: map[string]Element{}, lastActivity: time.Now()}
	chromedp.ListenTarget(ctx, func(event any) {
		t.recordEvent(event)
	})
	return t
}

func (t *Tab) recordEvent(event any) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastActivity = now
	switch e := event.(type) {
	case *cdpruntime.EventConsoleAPICalled:
		parts := make([]string, 0, len(e.Args))
		for _, arg := range e.Args {
			if arg == nil {
				continue
			}
			text := strings.TrimSpace(arg.Description)
			if text == "" {
				if data, err := json.Marshal(arg.Value); err == nil {
					text = strings.Trim(string(data), `"`)
				}
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		t.console = appendCapped(t.console, ConsoleEvent{At: now, Level: string(e.Type), Text: truncate(strings.Join(parts, " "), 4000)}, maxConsoleEvents)
	case *cdpruntime.EventExceptionThrown:
		text := "JavaScript exception"
		if e.ExceptionDetails != nil {
			text = e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
				text += ": " + e.ExceptionDetails.Exception.Description
			}
		}
		t.console = appendCapped(t.console, ConsoleEvent{At: now, Level: "exception", Text: truncate(text, 4000)}, maxConsoleEvents)
	case *network.EventRequestWillBeSent:
		id := string(e.RequestID)
		record := NetworkEvent{At: now, RequestID: id, Method: e.Request.Method, URL: e.Request.URL}
		t.requests[id] = record
		t.network = appendCapped(t.network, record, maxNetworkEvents)
	case *network.EventResponseReceived:
		id := string(e.RequestID)
		record := t.requests[id]
		record.At = now
		record.RequestID = id
		record.URL = e.Response.URL
		record.Status = e.Response.Status
		record.MIMEType = e.Response.MimeType
		t.requests[id] = record
		t.network = appendCapped(t.network, record, maxNetworkEvents)
	case *network.EventLoadingFailed:
		id := string(e.RequestID)
		record := t.requests[id]
		record.At = now
		record.RequestID = id
		record.Failed = true
		record.ErrorText = e.ErrorText
		t.requests[id] = record
		t.network = appendCapped(t.network, record, maxNetworkEvents)
	case *cdpruntime.EventExecutionContextsCleared:
		// Script IDs, DOM refs and snapshots are bound to the document that
		// produced them. CDP invalidates those IDs when all execution contexts
		// are cleared during a navigation or reload, so keeping them would make
		// scripts/search_source expose stale identifiers.
		t.resetDocumentStateLocked()
	case *debugger.EventScriptParsed:
		id := string(e.ScriptID)
		contextType, frameID, defaultContext := parseScriptContextAuxData([]byte(e.ExecutionContextAuxData))
		t.scripts[id] = ScriptInfo{
			ID:                 id,
			URL:                e.URL,
			Hash:               strings.ToLower(strings.TrimSpace(e.Hash)),
			Length:             e.Length,
			DocumentGeneration: t.documentGeneration,
			ExecutionContextID: int64(e.ExecutionContextID),
			ContextType:        contextType,
			FrameID:            frameID,
			DefaultContext:     defaultContext,
			SourceMapURL:       e.SourceMapURL,
			HasSourceURL:       e.HasSourceURL,
			IsModule:           e.IsModule,
			MappingSource:      "debugger_event",
		}
		if len(t.scripts) > maxScripts {
			for key := range t.scripts {
				delete(t.scripts, key)
				break
			}
		}
	}
}

func parseScriptContextAuxData(raw []byte) (contextType, frameID string, defaultContext bool) {
	if len(raw) == 0 {
		return "", "", false
	}
	var aux struct {
		IsDefault bool   `json:"isDefault"`
		Type      string `json:"type"`
		FrameID   string `json:"frameId"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(aux.Type), strings.TrimSpace(aux.FrameID), aux.IsDefault
}

func (t *Tab) resetDocumentStateLocked() {
	t.documentGeneration++
	t.scripts = map[string]ScriptInfo{}
	t.refs = map[string]string{}
	t.selectorRefs = map[string]string{}
	t.nextRef = 1
	t.lastElements = map[string]Element{}
	t.lastSnapshot = nil
	t.lastTitle = ""
	t.lastURL = ""
}

func (s *Session) Info() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info := s.info
	info.CurrentTabID = s.currentTab
	info.TabCount = len(s.tabs)
	// attached reports whether the persistent CDP context is still alive.
	// remote_attached separately indicates whether Lilith adopted an external
	// browser instead of launching one itself.
	info.Attached = !s.closed && s.browserCtx != nil && s.browserCtx.Err() == nil
	return info
}

func (s *Session) CurrentTab() (*Tab, error) {
	s.mu.RLock()
	tab := s.tabs[s.currentTab]
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return nil, errors.New("la sesión de navegador está cerrada")
	}
	if tab == nil {
		return nil, errors.New("la sesión no tiene una pestaña activa")
	}
	return tab, nil
}

func (s *Session) Touch() {
	s.mu.Lock()
	s.info.LastActivity = time.Now()
	s.mu.Unlock()
}

func (s *Session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	stop := s.stop
	allocStop := s.allocStop
	tempDir := s.tempDir
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	if allocStop != nil {
		allocStop()
	}
	if tempDir != "" {
		_ = os.RemoveAll(tempDir)
	}
}

func (s *Session) Tabs(ctx context.Context) ([]TabInfo, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := operationContext(ctx, tab.ctx, 10*time.Second)
	defer cancel()
	infos, err := chromedp.Targets(opCtx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	current := s.currentTab
	s.mu.RUnlock()
	out := make([]TabInfo, 0, len(infos))
	for _, info := range infos {
		if info.Type != "page" {
			continue
		}
		out = append(out, TabInfo{ID: string(info.TargetID), Title: info.Title, URL: info.URL, Selected: string(info.TargetID) == current})
	}
	return out, nil
}

func (s *Session) NewTab(ctx context.Context, url string) (TabInfo, error) {
	current, err := s.CurrentTab()
	if err != nil {
		return TabInfo{}, err
	}
	if strings.TrimSpace(url) == "" {
		url = "about:blank"
	}
	var id target.ID
	if err := runBrowserCommand(ctx, current.ctx, 10*time.Second, func(browserCtx context.Context) error {
		var createErr error
		id, createErr = target.CreateTarget(url).Do(browserCtx)
		return createErr
	}); err != nil {
		return TabInfo{}, err
	}
	tabCtx, cancel := chromedp.NewContext(s.browserCtx, chromedp.WithTargetID(id))
	tab := s.newTab(tabCtx, cancel, string(id))
	// This is the first Run for tabCtx, so it must use the persistent target
	// context directly. A timeout child cancelled after startup would cancel the
	// target and break the next action with context canceled.
	if err := runInitial(tabCtx, cancel, 20*time.Second, network.Enable(), cdpruntime.Enable(), enableDebugger()); err != nil {
		cancel()
		return TabInfo{}, fmt.Errorf("habilitar depuración de la pestaña: %w", err)
	}
	s.mu.Lock()
	s.tabs[string(id)] = tab
	s.currentTab = string(id)
	s.info.CurrentTabID = string(id)
	s.info.TabCount = len(s.tabs)
	s.info.LastActivity = time.Now()
	s.mu.Unlock()
	return TabInfo{ID: string(id), URL: url, Selected: true}, nil
}

func (s *Session) SwitchTab(ctx context.Context, id string) (TabInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TabInfo{}, errors.New("tab_id es obligatorio")
	}

	s.mu.RLock()
	tab := s.tabs[id]
	s.mu.RUnlock()
	created := false
	if tab == nil {
		tabCtx, cancel := chromedp.NewContext(s.browserCtx, chromedp.WithTargetID(target.ID(id)))
		tab = s.newTab(tabCtx, cancel, id)
		created = true
	}

	var title, url string
	if created {
		// A newly adopted target needs its first Run on its own persistent
		// context. This also enables the debug domains before the tab is exposed
		// through the session map.
		if err := runInitial(tab.ctx, tab.cancel, 20*time.Second,
			network.Enable(),
			cdpruntime.Enable(),
			enableDebugger(),
			chromedp.Title(&title),
			chromedp.Location(&url),
		); err != nil {
			if tab.cancel != nil {
				tab.cancel()
			}
			return TabInfo{}, fmt.Errorf("adjuntar pestaña %s: %w", id, err)
		}
	} else if err := runWithRequestTimeout(ctx, tab.ctx, 20*time.Second,
		chromedp.Title(&title),
		chromedp.Location(&url),
	); err != nil {
		return TabInfo{}, err
	}

	s.mu.Lock()
	if created {
		s.tabs[id] = tab
	}
	s.currentTab = id
	s.info.CurrentTabID = id
	s.info.TabCount = len(s.tabs)
	s.info.LastActivity = time.Now()
	s.mu.Unlock()
	return TabInfo{ID: id, Title: title, URL: url, Selected: true}, nil
}

func (s *Session) CloseTab(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		s.mu.RLock()
		id = s.currentTab
		s.mu.RUnlock()
	}
	current, err := s.CurrentTab()
	if err != nil {
		return err
	}
	if err := runBrowserCommand(ctx, current.ctx, 10*time.Second, func(browserCtx context.Context) error {
		return target.CloseTarget(target.ID(id)).Do(browserCtx)
	}); err != nil {
		return err
	}
	s.mu.Lock()
	if tab := s.tabs[id]; tab != nil && tab.cancel != nil {
		tab.cancel()
	}
	delete(s.tabs, id)
	if s.currentTab == id {
		s.currentTab = ""
		for next := range s.tabs {
			s.currentTab = next
			break
		}
	}
	s.info.CurrentTabID = s.currentTab
	s.info.TabCount = len(s.tabs)
	s.info.LastActivity = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *Session) Navigate(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("url es obligatoria")
	}
	tab, err := s.CurrentTab()
	if err != nil {
		return err
	}
	if err := runWithRequestTimeout(ctx, tab.ctx, 45*time.Second, chromedp.Navigate(url)); err != nil {
		return err
	}
	s.Touch()
	return nil
}

func sanitizeProfileName(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}

func validateSessionID(value string) error {
	if len(value) > 80 {
		return errors.New("session_id no puede exceder 80 caracteres")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("session_id contiene un carácter no permitido: %q", r)
	}
	return nil
}

func newID(prefix string) string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(data[:])
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "…"
}

func appendCapped[T any](values []T, value T, max int) []T {
	values = append(values, value)
	if len(values) <= max {
		return values
	}
	copy(values, values[len(values)-max:])
	return values[:max]
}
