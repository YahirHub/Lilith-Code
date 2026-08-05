package sshremote

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ServersFileName = "ssh-servers.json"
	serversVersion  = 1
)

type ServerProfile struct {
	ID                    string `json:"id"`
	Name                  string `json:"name,omitempty"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	PasswordEnv           string `json:"password_env,omitempty"`
	PasswordVault         bool   `json:"password_vault,omitempty"`
	PrivateKeyPath        string `json:"private_key_path,omitempty"`
	PassphraseEnv         string `json:"passphrase_env,omitempty"`
	PassphraseVault       bool   `json:"passphrase_vault,omitempty"`
	Agent                 string `json:"agent,omitempty"`
	AgentEnv              string `json:"agent_env,omitempty"`
	HostFingerprintSHA256 string `json:"host_fingerprint_sha256,omitempty"`
	ReadyTimeoutMS        int    `json:"ready_timeout_ms,omitempty"`
	KeepaliveIntervalMS   int    `json:"keepalive_interval_ms,omitempty"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

type ServerInput struct {
	Name                  string
	Host                  string
	Port                  int
	Username              string
	PasswordEnv           string
	PasswordVault         bool
	PrivateKeyPath        string
	PassphraseEnv         string
	PassphraseVault       bool
	Agent                 string
	AgentEnv              string
	HostFingerprintSHA256 string
	ReadyTimeoutMS        int
	KeepaliveIntervalMS   int
}

type ServerPatch struct {
	ServerInput
	// Fields carries explicitly supplied update values, including empty strings
	// used to clear individual optional settings. Supported keys mirror the
	// snake_case JSON profile fields.
	Fields               map[string]any
	ClearName            bool
	ClearAuthentication  bool
	ClearPasswordVault   bool
	ClearPassphraseVault bool
}

type serversFile struct {
	Version int             `json:"version"`
	Servers []ServerProfile `json:"servers"`
}

type ServerStore struct{ dir, path string }

func NewServerStore(dir string) *ServerStore {
	return &ServerStore{dir: dir, path: filepath.Join(dir, ServersFileName)}
}
func (s *ServerStore) Path() string { return s.path }

func nowISO() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func (s *ServerStore) load() ([]ServerProfile, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		Version int              `json:"version"`
		Servers []map[string]any `json:"servers"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("archivo de servidores SSH dañado: %w", err)
	}
	if raw.Version != serversVersion {
		return nil, fmt.Errorf("versión de servidores SSH no compatible: %d", raw.Version)
	}
	out := make([]ServerProfile, 0, len(raw.Servers))
	for i, r := range raw.Servers {
		p, err := profileFromMap(r)
		if err != nil {
			return nil, fmt.Errorf("servidor SSH %d: %w", i, err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(DisplayName(out[i])) < strings.ToLower(DisplayName(out[j]))
	})
	return out, nil
}

func profileFromMap(r map[string]any) (ServerProfile, error) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := r[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	host, user := get("host"), get("username")
	if host == "" || user == "" {
		return ServerProfile{}, errors.New("host y username son obligatorios")
	}
	port := intNum(r["port"], 22)
	if port < 1 || port > 65535 {
		return ServerProfile{}, errors.New("puerto fuera de rango")
	}
	created := get("created_at", "createdAt")
	if created == "" {
		created = nowISO()
	}
	updated := get("updated_at", "updatedAt")
	if updated == "" {
		updated = created
	}
	id := get("id", "server_id")
	if id == "" {
		id = newID("server")
	}
	name := get("name", "label")
	if len([]rune(name)) > 80 {
		return ServerProfile{}, errors.New("el nombre supera 80 caracteres")
	}
	p := ServerProfile{ID: id, Name: name, Host: host, Port: port, Username: user, PasswordEnv: get("password_env", "passwordEnv"), PrivateKeyPath: get("private_key_path", "privateKeyPath"), PassphraseEnv: get("passphrase_env", "passphraseEnv"), Agent: get("agent"), AgentEnv: get("agent_env", "agentEnv"), HostFingerprintSHA256: get("host_fingerprint_sha256", "hostFingerprintSha256"), ReadyTimeoutMS: intNum(first(r, "ready_timeout_ms", "readyTimeoutMs"), 0), KeepaliveIntervalMS: intNum(first(r, "keepalive_interval_ms", "keepaliveIntervalMs"), 0), CreatedAt: created, UpdatedAt: updated}
	p.PasswordVault = boolNum(first(r, "password_vault", "passwordVault"))
	p.PassphraseVault = boolNum(first(r, "passphrase_vault", "passphraseVault"))
	return normalizeProfile(p)
}
func first(r map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := r[k]; ok {
			return v
		}
	}
	return nil
}
func intNum(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return def
}
func boolNum(v any) bool { b, _ := v.(bool); return b }

func normalizeProfile(p ServerProfile) (ServerProfile, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	if p.ID == "" {
		p.ID = newID("server")
	}
	if p.Host == "" || p.Username == "" {
		return p, errors.New("host y username son obligatorios")
	}
	if p.Port == 0 {
		p.Port = 22
	}
	if p.Port < 1 || p.Port > 65535 {
		return p, errors.New("puerto fuera de rango")
	}
	if len([]rune(p.Name)) > 80 {
		return p, errors.New("el nombre supera 80 caracteres")
	}
	if isProtectedEnvPath(p.PrivateKeyPath) {
		return p, errors.New("un archivo .env protegido no puede usarse como clave privada")
	}
	if p.ReadyTimeoutMS != 0 && (p.ReadyTimeoutMS < 1000 || p.ReadyTimeoutMS > 120000) {
		return p, errors.New("ready_timeout_ms fuera de rango")
	}
	if p.KeepaliveIntervalMS != 0 && (p.KeepaliveIntervalMS < 1000 || p.KeepaliveIntervalMS > 120000) {
		return p, errors.New("keepalive_interval_ms fuera de rango")
	}
	return p, nil
}

func (s *ServerStore) save(profiles []ServerProfile) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(serversFile{Version: serversVersion, Servers: profiles}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.path, append(data, '\n'), 0600)
}

func (s *ServerStore) List() ([]ServerProfile, error) { return s.load() }
func (s *ServerStore) Get(ref string) (ServerProfile, error) {
	profiles, err := s.load()
	if err != nil {
		return ServerProfile{}, err
	}
	ref = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(ref), "ssh-server://"))
	var matches []ServerProfile
	for _, p := range profiles {
		if matchServer(p, ref) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return ServerProfile{}, fmt.Errorf("servidor SSH no encontrado: %s", ref)
	}
	if len(matches) > 1 {
		return ServerProfile{}, fmt.Errorf("referencia SSH ambigua: %s", ref)
	}
	return matches[0], nil
}
func matchServer(p ServerProfile, ref string) bool {
	vals := []string{p.ID, p.Name, p.Host, fmt.Sprintf("%s:%d", p.Host, p.Port), p.Username + "@" + p.Host}
	for _, v := range vals {
		if strings.EqualFold(strings.TrimSpace(v), ref) {
			return true
		}
	}
	return false
}
func DisplayName(p ServerProfile) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Username + "@" + p.Host
}
func (s *ServerStore) Add(in ServerInput) (ServerProfile, error) {
	p := ServerProfile{ID: newID("server"), Name: in.Name, Host: in.Host, Port: in.Port, Username: in.Username, PasswordEnv: in.PasswordEnv, PasswordVault: in.PasswordVault, PrivateKeyPath: in.PrivateKeyPath, PassphraseEnv: in.PassphraseEnv, PassphraseVault: in.PassphraseVault, Agent: in.Agent, AgentEnv: in.AgentEnv, HostFingerprintSHA256: in.HostFingerprintSHA256, ReadyTimeoutMS: in.ReadyTimeoutMS, KeepaliveIntervalMS: in.KeepaliveIntervalMS, CreatedAt: nowISO(), UpdatedAt: nowISO()}
	var err error
	p, err = normalizeProfile(p)
	if err != nil {
		return p, err
	}
	profiles, err := s.load()
	if err != nil {
		return p, err
	}
	for _, e := range profiles {
		if p.Name != "" && strings.EqualFold(e.Name, p.Name) {
			return p, fmt.Errorf("ya existe un servidor llamado %s", p.Name)
		}
	}
	profiles = append(profiles, p)
	err = s.save(profiles)
	return p, err
}
func (s *ServerStore) Update(ref string, patch ServerPatch) (ServerProfile, error) {
	profiles, err := s.load()
	if err != nil {
		return ServerProfile{}, err
	}
	target, err := s.Get(ref)
	if err != nil {
		return ServerProfile{}, err
	}
	for i, p := range profiles {
		if p.ID != target.ID {
			continue
		}
		if patch.ClearName {
			p.Name = ""
		}
		if patch.ClearAuthentication {
			p.PasswordEnv = ""
			p.PasswordVault = false
			p.PrivateKeyPath = ""
			p.PassphraseEnv = ""
			p.PassphraseVault = false
			p.Agent = ""
			p.AgentEnv = ""
		}
		if patch.ClearPasswordVault {
			p.PasswordVault = false
		}
		if patch.ClearPassphraseVault {
			p.PassphraseVault = false
		}
		in := patch.ServerInput
		if in.Name != "" {
			p.Name = in.Name
		}
		if in.Host != "" {
			p.Host = in.Host
		}
		if in.Port != 0 {
			p.Port = in.Port
		}
		if in.Username != "" {
			p.Username = in.Username
		}
		if in.PasswordEnv != "" {
			p.PasswordEnv = in.PasswordEnv
		}
		if in.PasswordVault {
			p.PasswordVault = true
		}
		if in.PrivateKeyPath != "" {
			p.PrivateKeyPath = in.PrivateKeyPath
		}
		if in.PassphraseEnv != "" {
			p.PassphraseEnv = in.PassphraseEnv
		}
		if in.PassphraseVault {
			p.PassphraseVault = true
		}
		if in.Agent != "" {
			p.Agent = in.Agent
		}
		if in.AgentEnv != "" {
			p.AgentEnv = in.AgentEnv
		}
		if in.HostFingerprintSHA256 != "" {
			p.HostFingerprintSHA256 = in.HostFingerprintSHA256
		}
		if in.ReadyTimeoutMS != 0 {
			p.ReadyTimeoutMS = in.ReadyTimeoutMS
		}
		if in.KeepaliveIntervalMS != 0 {
			p.KeepaliveIntervalMS = in.KeepaliveIntervalMS
		}
		for key, value := range patch.Fields {
			switch key {
			case "name":
				p.Name, _ = value.(string)
			case "host":
				p.Host, _ = value.(string)
			case "port":
				p.Port = intNum(value, p.Port)
			case "username":
				p.Username, _ = value.(string)
			case "password_env":
				p.PasswordEnv, _ = value.(string)
			case "private_key_path":
				p.PrivateKeyPath, _ = value.(string)
			case "passphrase_env":
				p.PassphraseEnv, _ = value.(string)
			case "agent":
				p.Agent, _ = value.(string)
			case "agent_env":
				p.AgentEnv, _ = value.(string)
			case "host_fingerprint_sha256":
				p.HostFingerprintSHA256, _ = value.(string)
			case "ready_timeout_ms":
				p.ReadyTimeoutMS = intNum(value, p.ReadyTimeoutMS)
			case "keepalive_interval_ms":
				p.KeepaliveIntervalMS = intNum(value, p.KeepaliveIntervalMS)
			}
		}
		p.UpdatedAt = nowISO()
		p, err = normalizeProfile(p)
		if err != nil {
			return p, err
		}
		profiles[i] = p
		if err = s.save(profiles); err != nil {
			return p, err
		}
		return p, nil
	}
	return ServerProfile{}, errors.New("servidor SSH no encontrado")
}
func (s *ServerStore) Rename(ref, name string, clear bool) (ServerProfile, error) {
	return s.Update(ref, ServerPatch{ServerInput: ServerInput{Name: name}, ClearName: clear})
}
func (s *ServerStore) Delete(ref string) (ServerProfile, error) {
	profiles, err := s.load()
	if err != nil {
		return ServerProfile{}, err
	}
	target, err := s.Get(ref)
	if err != nil {
		return ServerProfile{}, err
	}
	out := profiles[:0]
	for _, p := range profiles {
		if p.ID != target.ID {
			out = append(out, p)
		}
	}
	if err = s.save(out); err != nil {
		return target, err
	}
	return target, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".lilith-ssh-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	_ = os.Chmod(path, mode)
	return nil
}
func isProtectedEnvPath(p string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(p)))
	if base == ".env" {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		for _, s := range []string{"example", "sample", "template", "dist", "defaults"} {
			if base == ".env."+s {
				return false
			}
		}
		return true
	}
	return false
}
