package sshremote

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/interaction"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type ConfirmPrompt func(ctx context.Context, title, message string) (bool, error)

type Manager struct {
	mu          sync.Mutex
	configDir   string
	store       *ServerStore
	vault       *CredentialVault
	prompt      SecretPrompt
	confirm     ConfirmPrompt
	connections map[string]*Connection
}

type Connection struct {
	mu               sync.Mutex
	opMu             sync.Mutex
	reconnectMu      sync.Mutex
	manager          *Manager
	connectOptions   ConnectOptions
	ID               string
	ServerID         string
	DisplayName      string
	Host             string
	Port             int
	Username         string
	CWD              string
	CreatedAt        time.Time
	LastUsedAt       time.Time
	LastReconnectAt  time.Time
	LastTransportErr string
	ReconnectCount   int
	Generation       uint64
	transportHealthy bool
	client           *ssh.Client
	sftp             *sftp.Client
	agentConn        net.Conn
	keepaliveCancel  context.CancelFunc
	shells           map[string]*RemoteShell
	currentShell     string
	closed           bool
}

type RemoteShell struct {
	mu           sync.Mutex
	ID           string
	session      *ssh.Session
	stdin        io.WriteCloser
	buffer       []byte
	cursor       int
	stdout       []byte
	stderr       []byte
	stdoutCursor int
	stderrCursor int
	maxBuffer    int
	done         chan struct{}
	exitErr      error
}

type ConnectOptions struct {
	Profile                *ServerProfile
	Host                   string
	Port                   int
	Username               string
	Password               string
	PasswordEnv            string
	PrivateKeyPath         string
	PrivateKey             string
	Passphrase             string
	PassphraseEnv          string
	Agent                  string
	AgentEnv               string
	HostFingerprintSHA256  string
	ReadyTimeoutMS         int
	KeepaliveIntervalMS    int
	PromptPassword         bool
	PromptPassphrase       bool
	OverrideAuthentication bool
}

type ExecResult struct {
	Stdout               string `json:"stdout"`
	Stderr               string `json:"stderr"`
	ExitCode             int    `json:"exit_code"`
	ExitStatusKnown      bool   `json:"exit_status_known"`
	TimedOut             bool   `json:"timed_out"`
	DurationMS           int64  `json:"duration_ms"`
	StdoutTruncated      bool   `json:"stdout_truncated"`
	StderrTruncated      bool   `json:"stderr_truncated"`
	TransportRecovered   bool   `json:"transport_recovered"`
	ReconnectCount       int    `json:"reconnect_count"`
	ConnectionGeneration uint64 `json:"connection_generation"`
	TransportNotice      string `json:"transport_notice,omitempty"`
}
type RemoteFileInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at"`
	Type       string `json:"type"`
}

type ConnectionSnapshot struct {
	ID, ServerID, DisplayName, Host, Username, CWD string
	Port                                           int
	CreatedAt, LastUsedAt, LastReconnectAt         time.Time
	Closed, ShellOpen, TransportHealthy            bool
	ReconnectCount                                 int
	Generation                                     uint64
	LastTransportError                             string
}

var managers struct {
	sync.Mutex
	byDir map[string]*Manager
}

func init() { managers.byDir = map[string]*Manager{} }
func GetManager(configDir string, prompt SecretPrompt, confirm ConfirmPrompt) *Manager {
	clean := filepath.Clean(configDir)
	managers.Lock()
	defer managers.Unlock()
	m := managers.byDir[clean]
	if m == nil {
		m = &Manager{configDir: clean, store: NewServerStore(clean), prompt: prompt, confirm: confirm, connections: map[string]*Connection{}}
		m.vault = NewCredentialVault(clean, prompt)
		managers.byDir[clean] = m
	} else {
		if prompt != nil {
			m.prompt = prompt
			m.vault.SetPrompt(prompt)
		}
		if confirm != nil {
			m.confirm = confirm
		}
	}
	return m
}
func ShutdownAll() {
	managers.Lock()
	all := make([]*Manager, 0, len(managers.byDir))
	for _, m := range managers.byDir {
		all = append(all, m)
	}
	managers.byDir = map[string]*Manager{}
	managers.Unlock()
	for _, m := range all {
		m.CloseAll()
		m.vault.Lock()
	}
}
func (m *Manager) Store() *ServerStore     { return m.store }
func (m *Manager) Vault() *CredentialVault { return m.vault }

func (m *Manager) Connect(ctx context.Context, opt ConnectOptions) (*Connection, error) {
	resolved, name, serverID, err := m.normalizeConnectOptions(opt)
	if err != nil {
		return nil, err
	}
	client, agentConn, effective, err := m.dial(ctx, resolved)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	c := &Connection{
		manager:          m,
		connectOptions:   effective,
		ID:               newID("ssh"),
		ServerID:         serverID,
		Host:             resolved.Host,
		Port:             resolved.Port,
		Username:         resolved.Username,
		DisplayName:      name,
		CWD:              ".",
		CreatedAt:        now,
		LastUsedAt:       now,
		client:           client,
		agentConn:        agentConn,
		shells:           map[string]*RemoteShell{},
		transportHealthy: true,
		Generation:       1,
	}
	m.mu.Lock()
	m.connections[c.ID] = c
	m.mu.Unlock()
	c.watchTransport(client)
	kaCtx, cancel := context.WithCancel(context.Background())
	c.keepaliveCancel = cancel
	go c.keepalive(kaCtx, time.Duration(resolved.KeepaliveIntervalMS)*time.Millisecond)
	return c, nil
}

func (m *Manager) normalizeConnectOptions(opt ConnectOptions) (ConnectOptions, string, string, error) {
	if opt.Profile != nil {
		profile := *opt.Profile
		opt.Profile = &profile
		if strings.TrimSpace(opt.Host) == "" {
			opt.Host = profile.Host
		}
		if strings.TrimSpace(opt.Username) == "" {
			opt.Username = profile.Username
		}
		if opt.Port == 0 {
			opt.Port = profile.Port
		}
		if !opt.OverrideAuthentication {
			if opt.PasswordEnv == "" {
				opt.PasswordEnv = profile.PasswordEnv
			}
			if opt.PrivateKeyPath == "" {
				opt.PrivateKeyPath = profile.PrivateKeyPath
			}
			if opt.PassphraseEnv == "" {
				opt.PassphraseEnv = profile.PassphraseEnv
			}
			if opt.Agent == "" {
				opt.Agent = profile.Agent
			}
			if opt.AgentEnv == "" {
				opt.AgentEnv = profile.AgentEnv
			}
		}
		if opt.HostFingerprintSHA256 == "" {
			opt.HostFingerprintSHA256 = profile.HostFingerprintSHA256
		}
		if opt.ReadyTimeoutMS == 0 {
			opt.ReadyTimeoutMS = profile.ReadyTimeoutMS
		}
		if opt.KeepaliveIntervalMS == 0 {
			opt.KeepaliveIntervalMS = profile.KeepaliveIntervalMS
		}
	}
	opt.Host = strings.TrimSpace(opt.Host)
	opt.Username = strings.TrimSpace(opt.Username)
	if opt.Host == "" || opt.Username == "" {
		return ConnectOptions{}, "", "", errors.New("host y username son obligatorios")
	}
	if opt.Port == 0 {
		opt.Port = 22
	}
	if opt.ReadyTimeoutMS == 0 {
		opt.ReadyTimeoutMS = 30000
	}
	if opt.KeepaliveIntervalMS == 0 {
		opt.KeepaliveIntervalMS = 15000
	}
	name := opt.Username + "@" + opt.Host
	serverID := ""
	if opt.Profile != nil {
		name = DisplayName(*opt.Profile)
		serverID = opt.Profile.ID
	}
	return opt, name, serverID, nil
}

func (m *Manager) dial(ctx context.Context, opt ConnectOptions) (*ssh.Client, net.Conn, ConnectOptions, error) {
	methods, agentConn, err := m.authMethods(ctx, &opt)
	if err != nil {
		return nil, nil, ConnectOptions{}, err
	}
	if len(methods) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, ConnectOptions{}, errors.New("no se configuró un método de autenticación SSH")
	}
	hostKey := ssh.InsecureIgnoreHostKey() // Codewolf-compatible: pinning is opt-in.
	if fp := strings.TrimSpace(opt.HostFingerprintSHA256); fp != "" {
		expected := normalizeFingerprint(fp)
		hostKey = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			actualRaw := ssh.FingerprintSHA256(key)
			actual := normalizeFingerprint(actualRaw)
			if actual != expected {
				return fmt.Errorf("huella SSH no coincide para %s: esperada %s, recibida %s", hostname, fp, actualRaw)
			}
			return nil
		}
	}
	cfg := &ssh.ClientConfig{
		User:            opt.Username,
		Auth:            methods,
		HostKeyCallback: hostKey,
		Timeout:         time.Duration(opt.ReadyTimeoutMS) * time.Millisecond,
	}
	addr := net.JoinHostPort(opt.Host, fmt.Sprint(opt.Port))
	dialer := net.Dialer{Timeout: time.Duration(opt.ReadyTimeoutMS) * time.Millisecond}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, ConnectOptions{}, fmt.Errorf("conexión SSH a %s: %w", addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		_ = raw.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, ConnectOptions{}, fmt.Errorf("autenticación SSH: %w", err)
	}
	return ssh.NewClient(cc, chans, reqs), agentConn, opt, nil
}

func (m *Manager) authMethods(ctx context.Context, opt *ConnectOptions) ([]ssh.AuthMethod, net.Conn, error) {
	var out []ssh.AuthMethod
	password := opt.Password
	if password == "" && opt.PasswordEnv != "" {
		password = os.Getenv(opt.PasswordEnv)
		if password == "" {
			return nil, nil, fmt.Errorf("la variable de entorno %s está vacía o no existe", opt.PasswordEnv)
		}
	}
	passphrase := opt.Passphrase
	if passphrase == "" && opt.PassphraseEnv != "" {
		passphrase = os.Getenv(opt.PassphraseEnv)
		if passphrase == "" {
			return nil, nil, fmt.Errorf("la variable de entorno %s está vacía o no existe", opt.PassphraseEnv)
		}
	}
	if opt.Profile != nil && !opt.OverrideAuthentication && (opt.Profile.PasswordVault || opt.Profile.PassphraseVault) {
		secrets, ok, err := m.vault.GetForConnection(ctx, opt.Profile.ID, DisplayName(*opt.Profile))
		if err != nil {
			return nil, nil, err
		}
		if opt.Profile.PasswordVault && (!ok || secrets.Password == "") && password == "" {
			return nil, nil, fmt.Errorf("el perfil %s indica una contraseña cifrada, pero la bóveda no contiene esa credencial", DisplayName(*opt.Profile))
		}
		if opt.Profile.PassphraseVault && (!ok || secrets.Passphrase == "") && passphrase == "" {
			return nil, nil, fmt.Errorf("el perfil %s indica una passphrase cifrada, pero la bóveda no contiene esa credencial", DisplayName(*opt.Profile))
		}
		if ok {
			if password == "" {
				password = secrets.Password
			}
			if passphrase == "" {
				passphrase = secrets.Passphrase
			}
		}
	}
	if opt.PromptPassword && password == "" {
		if m.prompt == nil {
			return nil, nil, errors.New("entrada secreta no disponible")
		}
		v, err := m.prompt(ctx, interaction.SecretRemotePassword, "Contraseña del servidor remoto", "Escribe la contraseña de la cuenta SSH "+opt.Username+"@"+opt.Host+". Esta es la contraseña del servidor remoto, no la contraseña maestra de la bóveda SSH. No se enviará al modelo ni se guardará en el historial.", false, 1)
		if err != nil {
			return nil, nil, err
		}
		password = v
		// Conserva la credencial sólo en memoria durante la vida de esta conexión
		// lógica. Así una reparación automática no vuelve a pedirla.
		opt.Password = v
	}
	if password != "" {
		out = append(out, ssh.Password(password))
		password = ""
	}
	keyData := []byte(opt.PrivateKey)
	if len(keyData) == 0 && opt.PrivateKeyPath != "" {
		expanded, err := expandLocalPath(opt.PrivateKeyPath)
		if err != nil {
			return nil, nil, err
		}
		keyData, err = os.ReadFile(expanded)
		if err != nil {
			return nil, nil, fmt.Errorf("leer clave privada: %w", err)
		}
	}
	if len(keyData) > 0 {
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
			if _, ok := err.(*ssh.PassphraseMissingError); ok && opt.PromptPassphrase && m.prompt != nil {
				passphrase, e := m.prompt(ctx, interaction.SecretKeyPassphrase, "Passphrase de la clave privada SSH", "Escribe la passphrase que protege la clave privada SSH. No es la contraseña maestra de la bóveda ni la contraseña de la cuenta del servidor. No se enviará al modelo ni se guardará en el historial.", false, 1)
				if e != nil {
					return nil, nil, e
				}
				// Igual que la contraseña de cuenta solicitada por popup, la
				// passphrase vive sólo en esta conexión lógica para reconectar.
				opt.Passphrase = passphrase
				signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
			}
		}
		zero(keyData)
		if err != nil {
			return nil, nil, fmt.Errorf("clave privada SSH: %w", err)
		}
		out = append(out, ssh.PublicKeys(signer))
	}
	socket := strings.TrimSpace(opt.Agent)
	if socket == "" && opt.AgentEnv != "" {
		socket = os.Getenv(opt.AgentEnv)
		if socket == "" {
			return nil, nil, fmt.Errorf("la variable de entorno %s está vacía o no existe", opt.AgentEnv)
		}
	}
	if socket == "" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	}
	if socket == "" && defaultAgentEndpoint != "" {
		socket = defaultAgentEndpoint
	}
	var agentConn net.Conn
	if socket != "" {
		conn, err := dialAgent(ctx, socket)
		if err == nil {
			agentConn = conn
			out = append(out, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		} else if opt.Agent != "" || opt.AgentEnv != "" {
			return nil, nil, fmt.Errorf("agente SSH: %w", err)
		}
	}
	return out, agentConn, nil
}
func expandLocalPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		return filepath.Join(home, value[2:]), nil
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	abs, err := filepath.Abs(value)
	return abs, err
}

func normalizeFingerprint(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "SHA256:"), "sha256:"))
	return strings.TrimRight(value, "=")
}

func isSSHTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, marker := range []string{
		"eof",
		"without exit status or exit signal",
		"connection reset",
		"broken pipe",
		"closed network connection",
		"connection lost",
		"ssh: disconnect",
		"unexpected packet",
		"use of closed network connection",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func shortTransportError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 240 {
		text = text[:240] + "…"
	}
	return text
}

func stableTransportError(operation string) error {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "completar la operación"
	}
	return fmt.Errorf("Lilith no pudo %s después de varios intentos de recuperación; el connection_id lógico sigue reservado y no debe cerrarse ni reemplazarse manualmente", operation)
}

func (c *Connection) markTransportFailed(err error) {
	c.markTransportFailedFor(nil, err)
}

func (c *Connection) markTransportFailedFor(client *ssh.Client, err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	if c.closed || (client != nil && c.client != client) {
		c.mu.Unlock()
		return
	}
	c.transportHealthy = false
	c.LastTransportErr = shortTransportError(err)
	sf := c.sftp
	c.sftp = nil
	c.mu.Unlock()
	if sf != nil {
		_ = sf.Close()
	}
}

func (c *Connection) reconnect(ctx context.Context, cause error) error {
	if c == nil || c.manager == nil {
		return errors.New("la conexión SSH no tiene datos para reconectar")
	}
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("conexión SSH cerrada")
	}
	if c.transportHealthy && c.client != nil {
		c.mu.Unlock()
		return nil
	}
	opt := c.connectOptions
	c.mu.Unlock()

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 250 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		client, agentConn, effective, err := c.manager.dial(ctx, opt)
		if err != nil {
			last = err
			if !isSSHTransportError(err) {
				break
			}
			continue
		}

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			_ = client.Close()
			if agentConn != nil {
				_ = agentConn.Close()
			}
			return errors.New("conexión SSH cerrada")
		}
		oldClient := c.client
		oldSFTP := c.sftp
		oldAgent := c.agentConn
		oldShells := make([]*RemoteShell, 0, len(c.shells))
		for _, shell := range c.shells {
			oldShells = append(oldShells, shell)
		}
		c.client = client
		c.connectOptions = effective
		c.sftp = nil
		c.agentConn = agentConn
		c.shells = map[string]*RemoteShell{}
		c.currentShell = ""
		c.transportHealthy = true
		c.LastTransportErr = shortTransportError(cause)
		c.LastReconnectAt = time.Now()
		c.ReconnectCount++
		c.Generation++
		c.mu.Unlock()
		c.watchTransport(client)

		for _, shell := range oldShells {
			shell.Close()
		}
		if oldSFTP != nil {
			_ = oldSFTP.Close()
		}
		if oldClient != nil {
			_ = oldClient.Close()
		}
		if oldAgent != nil {
			_ = oldAgent.Close()
		}
		return nil
	}
	if last == nil {
		last = cause
	}
	if last == nil {
		last = errors.New("transporte SSH no disponible")
	}
	c.markTransportFailed(last)
	if isSSHTransportError(last) {
		return stableTransportError("restaurar el transporte SSH")
	}
	return fmt.Errorf("reconexión SSH automática: %w", last)
}

func (c *Connection) ensureConnected(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("conexión SSH cerrada")
	}
	healthy := c.transportHealthy && c.client != nil
	last := c.LastTransportErr
	c.mu.Unlock()
	if healthy {
		return nil
	}
	var cause error
	if last != "" {
		cause = errors.New(last)
	}
	return c.reconnect(ctx, cause)
}

// EnsureConnected keeps the logical connection ID stable while repairing its
// underlying SSH transport when the server or network closed it.
func (c *Connection) EnsureConnected(ctx context.Context) error {
	return c.ensureConnected(ctx)
}

func (c *Connection) newSession(ctx context.Context) (*ssh.Session, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.ensureConnected(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		client := c.client
		c.mu.Unlock()
		if client == nil {
			c.markTransportFailed(errors.New("cliente SSH no disponible"))
			continue
		}
		session, err := client.NewSession()
		if err == nil {
			return session, nil
		}
		if !isSSHTransportError(err) {
			return nil, err
		}
		c.markTransportFailed(err)
		if attempt == 2 {
			return nil, stableTransportError("abrir un canal SSH")
		}
		if err = c.reconnect(ctx, err); err != nil {
			return nil, err
		}
	}
	return nil, stableTransportError("abrir un canal SSH")
}

func (c *Connection) watchTransport(client *ssh.Client) {
	if c == nil || client == nil {
		return
	}
	go func() {
		err := client.Wait()
		if err == nil {
			err = io.EOF
		}
		// A watcher from an older generation can finish after reconnection. The
		// client identity guard prevents it from invalidating the replacement.
		c.markTransportFailedFor(client, err)
	}()
}

func (c *Connection) keepalive(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			client := c.client
			healthy := c.transportHealthy
			c.mu.Unlock()
			if client == nil || !healthy {
				continue
			}
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				// No se abre ningún prompt desde el keepalive. La siguiente acción
				// repara el transporte de forma transparente usando el mismo ID.
				// El guard evita que un keepalive tardío de una generación anterior
				// invalide el cliente recién reconectado.
				c.markTransportFailedFor(client, err)
			}
		}
	}
}
func normalizeConnRef(ref string) string { return strings.TrimSpace(strings.TrimPrefix(ref, "ssh://")) }

func (c *Connection) Snapshot() ConnectionSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	shellOpen := false
	if current := c.shells[c.currentShell]; current != nil {
		shellOpen = current.IsOpen()
	}
	return ConnectionSnapshot{
		ID: c.ID, ServerID: c.ServerID, DisplayName: c.DisplayName, Host: c.Host,
		Port: c.Port, Username: c.Username, CWD: c.CWD, CreatedAt: c.CreatedAt,
		LastUsedAt: c.LastUsedAt, LastReconnectAt: c.LastReconnectAt,
		Closed: c.closed, ShellOpen: shellOpen, TransportHealthy: c.transportHealthy,
		ReconnectCount: c.ReconnectCount, Generation: c.Generation,
		LastTransportError: c.LastTransportErr,
	}
}

func (c *Connection) AttachServer(serverID, displayName string) {
	c.mu.Lock()
	c.ServerID = serverID
	c.DisplayName = displayName
	c.mu.Unlock()
}

func (c *Connection) detachServer(serverID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ServerID != serverID {
		return false
	}
	c.ServerID = ""
	return true
}

func (c *Connection) renameServer(serverID, displayName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ServerID != serverID {
		return false
	}
	c.DisplayName = displayName
	return true
}

func (m *Manager) connectionPointers() []*Connection {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Connection, 0, len(m.connections))
	for _, c := range m.connections {
		out = append(out, c)
	}
	return out
}

func (m *Manager) Get(ref string) (*Connection, error) {
	id := normalizeConnRef(ref)
	m.mu.Lock()
	c := m.connections[id]
	m.mu.Unlock()
	if c != nil && !c.Snapshot().Closed {
		return c, nil
	}
	return nil, fmt.Errorf("conexión SSH no encontrada: %s", ref)
}
func (m *Manager) ListConnections() []map[string]any {
	connections := m.connectionPointers()
	out := make([]map[string]any, 0, len(connections))
	for _, c := range connections {
		s := c.Snapshot()
		if s.Closed {
			continue
		}
		item := map[string]any{
			"connection_id": s.ID, "connection_ref": "ssh://" + s.ID, "ref": "ssh://" + s.ID,
			"name": s.DisplayName, "label": s.DisplayName, "host": s.Host, "port": s.Port,
			"username": s.Username, "cwd": s.CWD, "shell_open": s.ShellOpen,
			"connected_at": s.CreatedAt.UTC().Format(time.RFC3339), "created_at": s.CreatedAt.UTC().Format(time.RFC3339),
			"last_used_at": s.LastUsedAt.UTC().Format(time.RFC3339), "connected": true,
			"transport_connected": s.TransportHealthy, "connection_generation": s.Generation,
			"reconnect_count": s.ReconnectCount,
		}
		if !s.LastReconnectAt.IsZero() {
			item["last_reconnect_at"] = s.LastReconnectAt.UTC().Format(time.RFC3339)
		}
		if s.LastTransportError != "" {
			item["last_transport_error"] = s.LastTransportError
		}
		if s.ServerID != "" {
			item["server_id"] = s.ServerID
			item["server_ref"] = "ssh-server://" + s.ServerID
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]["connection_id"]) < fmt.Sprint(out[j]["connection_id"]) })
	return out
}
func (m *Manager) Close(ref string) error {
	c, err := m.Get(ref)
	if err != nil {
		return err
	}
	c.close()
	m.mu.Lock()
	delete(m.connections, c.ID)
	m.mu.Unlock()
	return nil
}
func (m *Manager) CloseByServer(serverID string) {
	for _, c := range m.connectionPointers() {
		if c.Snapshot().ServerID == serverID {
			_ = m.Close(c.ID)
		}
	}
}
func (m *Manager) DetachServer(serverID string) int {
	count := 0
	for _, c := range m.connectionPointers() {
		if c.detachServer(serverID) {
			count++
		}
	}
	return count
}

func (m *Manager) RenameServerConnections(serverID, displayName string) int {
	count := 0
	for _, c := range m.connectionPointers() {
		if c.renameServer(serverID, displayName) {
			count++
		}
	}
	return count
}
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Connection, 0, len(m.connections))
	for _, c := range m.connections {
		all = append(all, c)
	}
	m.connections = map[string]*Connection{}
	m.mu.Unlock()
	for _, c := range all {
		c.close()
	}
}
func (c *Connection) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.transportHealthy = false
	if c.keepaliveCancel != nil {
		c.keepaliveCancel()
	}
	shells := make([]*RemoteShell, 0, len(c.shells))
	for _, s := range c.shells {
		shells = append(shells, s)
	}
	c.shells = map[string]*RemoteShell{}
	sf := c.sftp
	c.sftp = nil
	client := c.client
	agentConn := c.agentConn
	// Las credenciales guardadas en las opciones sólo sirven para reparar el
	// transporte de esta conexión lógica. Se eliminan al cerrarla.
	c.connectOptions.Password = ""
	c.connectOptions.Passphrase = ""
	c.connectOptions.PrivateKey = ""
	c.mu.Unlock()
	for _, s := range shells {
		s.Close()
	}
	if sf != nil {
		sf.Close()
	}
	if client != nil {
		client.Close()
	}
	if agentConn != nil {
		agentConn.Close()
	}
}

func (c *Connection) LockOperation() func() {
	c.opMu.Lock()
	c.mu.Lock()
	c.LastUsedAt = time.Now()
	c.mu.Unlock()
	return c.opMu.Unlock
}

func (c *Connection) Resolve(remote string) string { return c.resolve(remote) }
func (c *Connection) resolve(remote string) string {
	c.mu.Lock()
	cwd := c.CWD
	c.mu.Unlock()
	remote = strings.ReplaceAll(strings.TrimSpace(remote), "\\", "/")
	if remote == "" || remote == "." {
		return cwd
	}
	if strings.HasPrefix(remote, "/") {
		return path.Clean(remote)
	}
	if cwd == "." || cwd == "" {
		return path.Clean(remote)
	}
	return path.Clean(path.Join(cwd, remote))
}
func quotePOSIX(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

const maxExecCaptureBytes = 10_485_760

type boundedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 {
		b.max = maxExecCaptureBytes
	}
	remaining := b.max - b.buf.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || n > 0
		return n, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }

func (c *Connection) prepareExecSession(ctx context.Context, pty bool, cols, rows int) (*ssh.Session, error) {
	for attempt := 0; attempt < 3; attempt++ {
		session, err := c.newSession(ctx)
		if err != nil {
			return nil, err
		}
		if !pty {
			return session, nil
		}
		if cols == 0 {
			cols = 120
		}
		if rows == 0 {
			rows = 30
		}
		err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 0})
		if err == nil {
			return session, nil
		}
		_ = session.Close()
		if !isSSHTransportError(err) {
			return nil, err
		}
		c.markTransportFailed(err)
		if attempt == 2 {
			return nil, stableTransportError("preparar el terminal remoto")
		}
		if err = c.reconnect(ctx, err); err != nil {
			return nil, err
		}
	}
	return nil, stableTransportError("preparar el terminal remoto")
}

func (c *Connection) Exec(ctx context.Context, command string, timeout time.Duration, pty bool, cols, rows int) (ExecResult, error) {
	start := time.Now()
	session, err := c.prepareExecSession(ctx, pty, cols, rows)
	if err != nil {
		return ExecResult{}, err
	}
	defer session.Close()
	c.mu.Lock()
	cwd := c.CWD
	c.mu.Unlock()
	stdout := boundedBuffer{max: maxExecCaptureBytes}
	stderr := boundedBuffer{max: maxExecCaptureBytes}
	session.Stdout = &stdout
	session.Stderr = &stderr
	full := command
	if cwd != "" && cwd != "." {
		full = "cd -- " + quotePOSIX(cwd) + " && " + command
	}
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Run(full) }()
	var runErr error
	timed := false
	select {
	case runErr = <-done:
	case <-runCtx.Done():
		timed = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		_ = session.Close()
		runErr = runCtx.Err()
	}
	code := 0
	exitKnown := runErr == nil
	if runErr != nil {
		code = -1
		var ee *ssh.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitStatus()
			exitKnown = true
		}
	}
	result := ExecResult{
		Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code, ExitStatusKnown: exitKnown,
		TimedOut: timed, DurationMS: time.Since(start).Milliseconds(),
		StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	if timed {
		return result, fmt.Errorf("comando SSH agotó el tiempo")
	}
	if runErr != nil && isSSHTransportError(runErr) {
		c.markTransportFailed(runErr)
		if reconnectErr := c.reconnect(ctx, runErr); reconnectErr != nil {
			return result, fmt.Errorf("el transporte SSH se interrumpió y no pudo recuperarse: %w", reconnectErr)
		}
		snapshot := c.Snapshot()
		result.TransportRecovered = true
		result.ReconnectCount = snapshot.ReconnectCount
		result.ConnectionGeneration = snapshot.Generation
		result.TransportNotice = "El servidor cerró el canal sin enviar un estado de salida. Lilith restauró automáticamente el transporte y conservó el mismo connection_id; no cierres ni vuelvas a conectar. El comando pudo haber terminado, pero su código de salida no puede confirmarse sin una verificación posterior."
		return result, nil
	}
	if runErr != nil && code < 0 {
		return result, runErr
	}
	snapshot := c.Snapshot()
	result.ReconnectCount = snapshot.ReconnectCount
	result.ConnectionGeneration = snapshot.Generation
	return result, nil
}

func (c *Connection) Pwd(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		r, err := c.Exec(ctx, "pwd", 15*time.Second, false, 0, 0)
		if err != nil {
			return "", err
		}
		cwd := strings.TrimSpace(r.Stdout)
		if cwd != "" {
			c.mu.Lock()
			c.CWD = cwd
			c.mu.Unlock()
			return cwd, nil
		}
		// pwd is read-only: when its first channel died without an exit status,
		// it is safe to repeat it once on the repaired transport.
		if !r.TransportRecovered || attempt > 0 {
			break
		}
	}
	return "", errors.New("el servidor no devolvió el directorio después de recuperar la conexión")
}

func (c *Connection) CD(ctx context.Context, p string) (string, error) {
	target := c.resolve(p)
	for attempt := 0; attempt < 2; attempt++ {
		r, err := c.Exec(ctx, "cd -- "+quotePOSIX(target)+" && pwd", 15*time.Second, false, 0, 0)
		if err != nil {
			return "", err
		}
		cwd := strings.TrimSpace(r.Stdout)
		if cwd != "" {
			c.mu.Lock()
			c.CWD = cwd
			c.mu.Unlock()
			return cwd, nil
		}
		// The remote cd only affects the temporary exec channel, so repeating
		// this compound read is safe after an automatic repair.
		if !r.TransportRecovered || attempt > 0 {
			break
		}
	}
	return "", errors.New("el servidor no devolvió el directorio después de recuperar la conexión")
}

func (c *Connection) SFTP() (*sftp.Client, error) {
	// No imponemos aquí un timeout que incluya el tiempo humano del popup de
	// credenciales. El dial TCP/SSH conserva su propio ready_timeout_ms.
	ctx := context.Background()
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.ensureConnected(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, errors.New("conexión SSH cerrada")
		}
		if c.sftp != nil {
			sf := c.sftp
			c.mu.Unlock()
			return sf, nil
		}
		client := c.client
		c.mu.Unlock()
		if client == nil {
			c.markTransportFailed(errors.New("cliente SSH no disponible"))
			continue
		}
		sf, err := sftp.NewClient(client)
		if err == nil {
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				_ = sf.Close()
				return nil, errors.New("conexión SSH cerrada")
			}
			c.sftp = sf
			c.mu.Unlock()
			return sf, nil
		}
		if !isSSHTransportError(err) {
			return nil, err
		}
		c.markTransportFailed(err)
		if attempt == 2 {
			return nil, stableTransportError("abrir el canal SFTP")
		}
		if err = c.reconnect(ctx, err); err != nil {
			return nil, err
		}
	}
	return nil, stableTransportError("abrir el canal SFTP")
}

func remoteType(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	return "file"
}
func toRemoteInfo(base string, info os.FileInfo) RemoteFileInfo {
	return RemoteFileInfo{Name: info.Name(), Path: base, Size: info.Size(), Mode: info.Mode().String(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Type: remoteType(info)}
}
func withSFTPRetry[T any](c *Connection, operation string, fn func(*sftp.Client, int) (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < 2; attempt++ {
		sf, err := c.SFTP()
		if err != nil {
			return zero, err
		}
		value, err := fn(sf, attempt)
		if err == nil {
			return value, nil
		}
		if !isSSHTransportError(err) {
			return zero, err
		}
		c.markTransportFailed(err)
		ctx := context.Background()
		reconnectErr := c.reconnect(ctx, err)
		if reconnectErr != nil {
			return zero, reconnectErr
		}
	}
	return zero, stableTransportError(operation)
}

func (c *Connection) List(p string) ([]RemoteFileInfo, error) {
	target := c.resolve(p)
	return withSFTPRetry(c, "listar el directorio remoto", func(sf *sftp.Client, _ int) ([]RemoteFileInfo, error) {
		entries, err := sf.ReadDir(target)
		if err != nil {
			return nil, err
		}
		out := make([]RemoteFileInfo, 0, len(entries))
		for _, e := range entries {
			out = append(out, toRemoteInfo(path.Join(target, e.Name()), e))
		}
		return out, nil
	})
}

func (c *Connection) Stat(p string) (RemoteFileInfo, error) {
	target := c.resolve(p)
	return withSFTPRetry(c, "consultar el archivo remoto", func(sf *sftp.Client, _ int) (RemoteFileInfo, error) {
		info, err := sf.Lstat(target)
		if err != nil {
			return RemoteFileInfo{}, err
		}
		return toRemoteInfo(target, info), nil
	})
}

type remoteReadResult struct {
	content   string
	size      int64
	truncated bool
}

func (c *Connection) ReadFile(p, encoding string, max int) (content string, size int64, truncated bool, err error) {
	target := c.resolve(p)
	if max <= 0 {
		max = 200000
	}
	result, err := withSFTPRetry(c, "leer el archivo remoto", func(sf *sftp.Client, _ int) (remoteReadResult, error) {
		info, err := sf.Stat(target)
		if err != nil {
			return remoteReadResult{}, err
		}
		f, err := sf.Open(target)
		if err != nil {
			return remoteReadResult{}, err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, int64(max)))
		closeErr := f.Close()
		if readErr != nil {
			return remoteReadResult{}, readErr
		}
		if closeErr != nil {
			return remoteReadResult{}, closeErr
		}
		value := string(data)
		if encoding == "base64" {
			value = base64.StdEncoding.EncodeToString(data)
		}
		return remoteReadResult{content: value, size: info.Size(), truncated: info.Size() > int64(max)}, nil
	})
	if err != nil {
		return "", 0, false, err
	}
	return result.content, result.size, result.truncated, nil
}

func (c *Connection) WriteFile(p, content, encoding string, overwrite bool) error {
	target := c.resolve(p)
	data := []byte(content)
	var err error
	if encoding == "base64" {
		data, err = base64.StdEncoding.DecodeString(content)
		if err != nil {
			return err
		}
	}
	_, err = withSFTPRetry(c, "escribir el archivo remoto", func(sf *sftp.Client, attempt int) (struct{}, error) {
		if !overwrite && attempt == 0 {
			if _, statErr := sf.Lstat(target); statErr == nil {
				return struct{}{}, errors.New("el archivo remoto ya existe; usa overwrite=true")
			} else if isSSHTransportError(statErr) {
				return struct{}{}, statErr
			}
		}
		if err := sf.MkdirAll(path.Dir(target)); err != nil {
			return struct{}{}, err
		}
		f, err := sf.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
		if err != nil {
			return struct{}{}, err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return struct{}{}, writeErr
		}
		return struct{}{}, closeErr
	})
	return err
}

func (c *Connection) Mkdir(p string, recursive bool) error {
	target := c.resolve(p)
	_, err := withSFTPRetry(c, "crear el directorio remoto", func(sf *sftp.Client, attempt int) (struct{}, error) {
		if recursive {
			return struct{}{}, sf.MkdirAll(target)
		}
		if attempt > 0 {
			if info, statErr := sf.Stat(target); statErr == nil && info.IsDir() {
				return struct{}{}, nil
			} else if isSSHTransportError(statErr) {
				return struct{}{}, statErr
			}
		}
		return struct{}{}, sf.Mkdir(target)
	})
	return err
}

func (c *Connection) Rename(oldp, newp string, overwrite bool) error {
	oldp, newp = c.resolve(oldp), c.resolve(newp)
	_, err := withSFTPRetry(c, "renombrar el archivo remoto", func(sf *sftp.Client, attempt int) (struct{}, error) {
		if attempt > 0 {
			_, sourceErr := sf.Lstat(oldp)
			if os.IsNotExist(sourceErr) {
				if _, destinationErr := sf.Lstat(newp); destinationErr == nil {
					return struct{}{}, nil
				} else if isSSHTransportError(destinationErr) {
					return struct{}{}, destinationErr
				}
			} else if isSSHTransportError(sourceErr) {
				return struct{}{}, sourceErr
			}
		}
		if !overwrite && attempt == 0 {
			if _, statErr := sf.Lstat(newp); statErr == nil {
				return struct{}{}, errors.New("el destino remoto ya existe")
			} else if isSSHTransportError(statErr) {
				return struct{}{}, statErr
			}
		}
		return struct{}{}, sf.Rename(oldp, newp)
	})
	return err
}

func deleteRemotePath(sf *sftp.Client, target string, recursive bool) error {
	info, err := sf.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return sf.Remove(target)
	}
	if !recursive {
		return sf.RemoveDirectory(target)
	}
	walker := sf.Walk(target)
	var paths []string
	for walker.Step() {
		if walker.Err() != nil {
			return walker.Err()
		}
		paths = append(paths, walker.Path())
	}
	for i := len(paths) - 1; i >= 0; i-- {
		info, err := sf.Lstat(paths[i])
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.IsDir() {
			err = sf.RemoveDirectory(paths[i])
		} else {
			err = sf.Remove(paths[i])
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *Connection) Delete(p string, recursive bool) error {
	target := c.resolve(p)
	_, err := withSFTPRetry(c, "eliminar la ruta remota", func(sf *sftp.Client, attempt int) (struct{}, error) {
		err := deleteRemotePath(sf, target, recursive)
		if attempt > 0 && os.IsNotExist(err) {
			return struct{}{}, nil
		}
		return struct{}{}, err
	})
	return err
}

func (c *Connection) Upload(local, remote string, overwrite bool) error {
	local, err := expandLocalPath(local)
	if err != nil {
		return err
	}
	remote = c.resolve(remote)
	_, err = withSFTPRetry(c, "subir el archivo remoto", func(sf *sftp.Client, attempt int) (struct{}, error) {
		in, err := os.Open(local)
		if err != nil {
			return struct{}{}, err
		}
		defer in.Close()
		if !overwrite && attempt == 0 {
			if _, statErr := sf.Lstat(remote); statErr == nil {
				return struct{}{}, errors.New("el archivo remoto ya existe")
			} else if isSSHTransportError(statErr) {
				return struct{}{}, statErr
			}
		}
		if err = sf.MkdirAll(path.Dir(remote)); err != nil {
			return struct{}{}, err
		}
		out, err := sf.OpenFile(remote, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
		if err != nil {
			return struct{}{}, err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return struct{}{}, copyErr
		}
		return struct{}{}, closeErr
	})
	return err
}

func (c *Connection) Download(remote, local string, overwrite bool) error {
	remote = c.resolve(remote)
	local, err := expandLocalPath(local)
	if err != nil {
		return err
	}
	if !overwrite {
		if _, err = os.Stat(local); err == nil {
			return errors.New("el archivo local ya existe")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		return err
	}
	_, err = withSFTPRetry(c, "descargar el archivo remoto", func(sf *sftp.Client, _ int) (struct{}, error) {
		in, err := sf.Open(remote)
		if err != nil {
			return struct{}{}, err
		}
		out, err := os.OpenFile(local, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			_ = in.Close()
			return struct{}{}, err
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		outCloseErr := out.Close()
		if copyErr != nil {
			return struct{}{}, copyErr
		}
		if inCloseErr != nil {
			return struct{}{}, inCloseErr
		}
		return struct{}{}, outCloseErr
	})
	return err
}

func (c *Connection) OpenShell(cols, rows, maxBuffer int) (*RemoteShell, error) {
	c.mu.Lock()
	cwd := c.CWD
	c.mu.Unlock()
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 30
	}
	if maxBuffer <= 0 {
		maxBuffer = 1_048_576
	}
	// La apertura puede necesitar un popup de bóveda o de credencial. El timeout
	// de red se aplica dentro del dial, no al tiempo que tarda el usuario.
	ctx := context.Background()

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		session, err := c.newSession(ctx)
		if err != nil {
			return nil, err
		}
		if err = session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{}); err != nil {
			_ = session.Close()
			last = err
			if !isSSHTransportError(err) {
				return nil, err
			}
			c.markTransportFailed(err)
			if err = c.reconnect(ctx, err); err != nil {
				return nil, err
			}
			continue
		}
		stdin, err := session.StdinPipe()
		if err != nil {
			_ = session.Close()
			return nil, err
		}
		stdout, err := session.StdoutPipe()
		if err != nil {
			_ = session.Close()
			return nil, err
		}
		stderr, err := session.StderrPipe()
		if err != nil {
			_ = session.Close()
			return nil, err
		}
		if err = session.Shell(); err != nil {
			_ = session.Close()
			last = err
			if !isSSHTransportError(err) {
				return nil, err
			}
			c.markTransportFailed(err)
			if err = c.reconnect(ctx, err); err != nil {
				return nil, err
			}
			continue
		}
		sh := &RemoteShell{ID: newID("shell"), session: session, stdin: stdin, maxBuffer: maxBuffer, done: make(chan struct{})}
		c.mu.Lock()
		c.shells[sh.ID] = sh
		c.currentShell = sh.ID
		c.mu.Unlock()
		go sh.capture(stdout, false)
		go sh.capture(stderr, true)
		go func() {
			err := session.Wait()
			if isSSHTransportError(err) {
				c.markTransportFailed(err)
			}
			sh.mu.Lock()
			sh.exitErr = err
			sh.mu.Unlock()
			close(sh.done)
		}()
		if cwd != "" && cwd != "." {
			if err = sh.Write("cd -- " + quotePOSIX(cwd) + "\n"); err != nil {
				sh.Close()
				c.mu.Lock()
				delete(c.shells, sh.ID)
				if c.currentShell == sh.ID {
					c.currentShell = ""
				}
				c.mu.Unlock()
				if isSSHTransportError(err) {
					c.markTransportFailed(err)
					if reconnectErr := c.reconnect(ctx, err); reconnectErr != nil {
						return nil, reconnectErr
					}
					return nil, errors.New("la conexión SSH fue restaurada, pero la shell debe abrirse nuevamente")
				}
				return nil, err
			}
		}
		return sh, nil
	}
	if last == nil {
		last = errors.New("transporte SSH no disponible")
	}
	return nil, stableTransportError("abrir la shell remota")
}

func appendBoundedBuffer(dst *[]byte, cursor *int, data []byte, max int) {
	*dst = append(*dst, data...)
	if len(*dst) <= max {
		return
	}
	drop := len(*dst) - max
	*dst = append([]byte(nil), (*dst)[drop:]...)
	*cursor -= drop
	if *cursor < 0 {
		*cursor = 0
	}
}

func (s *RemoteShell) capture(r io.Reader, stderr bool) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.mu.Lock()
			chunk := buf[:n]
			appendBoundedBuffer(&s.buffer, &s.cursor, chunk, s.maxBuffer)
			if stderr {
				appendBoundedBuffer(&s.stderr, &s.stderrCursor, chunk, s.maxBuffer)
			} else {
				appendBoundedBuffer(&s.stdout, &s.stdoutCursor, chunk, s.maxBuffer)
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *RemoteShell) Write(data string) error {
	s.mu.Lock()
	stdin := s.stdin
	s.mu.Unlock()
	if stdin == nil {
		return errors.New("shell cerrado")
	}
	_, err := io.WriteString(stdin, data)
	return err
}

func consumeShellBytes(buffer []byte, cursor *int, max int) (string, bool) {
	start := *cursor
	end := len(buffer)
	available := end - start
	truncated := available > max
	readEnd := end
	if truncated {
		readEnd = start + max
	}
	text := string(buffer[start:readEnd])
	// Consume the entire pending chunk, matching Codewolf's incremental shell
	// behavior: bytes omitted by max_bytes are intentionally discarded.
	*cursor = end
	return text, truncated
}

func (s *RemoteShell) Read(max int) map[string]any {
	if max <= 0 {
		max = 200000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stdout, stdoutTruncated := consumeShellBytes(s.stdout, &s.stdoutCursor, max)
	stderr, stderrTruncated := consumeShellBytes(s.stderr, &s.stderrCursor, max)
	output, outputTruncated := consumeShellBytes(s.buffer, &s.cursor, max)
	done := false
	select {
	case <-s.done:
		done = true
	default:
	}
	result := map[string]any{
		"shell_id":         s.ID,
		"shell_ref":        "shell://" + s.ID,
		"output":           output,
		"output_truncated": outputTruncated,
		"stdout":           stdout,
		"stderr":           stderr,
		"stdout_truncated": stdoutTruncated,
		"stderr_truncated": stderrTruncated,
		"done":             done,
		"shell_open":       !done && s.stdin != nil,
		"cursor":           s.cursor,
	}
	if done && s.exitErr != nil {
		result["error"] = s.exitErr.Error()
	}
	return result
}

func (s *RemoteShell) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin == nil || s.session == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *RemoteShell) Close() {
	s.mu.Lock()
	if s.stdin != nil {
		_ = s.stdin.Close()
		s.stdin = nil
	}
	session := s.session
	s.session = nil
	s.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}
func (c *Connection) GetShell(ref string) (*RemoteShell, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(ref, "shell://"))
	c.mu.Lock()
	defer c.mu.Unlock()
	if ref == "" {
		ref = c.currentShell
	}
	s := c.shells[ref]
	if s == nil {
		return nil, fmt.Errorf("shell remoto no encontrado: %s", ref)
	}
	return s, nil
}
