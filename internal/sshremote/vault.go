package sshremote

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lilith/li/internal/interaction"
	"golang.org/x/crypto/scrypt"
)

const (
	SecretsFileName   = "ssh-secrets.enc"
	envelopeVersion   = 1
	payloadVersion    = 1
	keyLength         = 32
	scryptN           = 32768
	scryptR           = 8
	scryptP           = 1
	maxUnlockAttempts = 3
)

var vaultAAD = []byte("lilith:ssh-secrets:v1")

type SecretPrompt func(ctx context.Context, secretKind interaction.SecretKind, title, message string, confirm bool, minLength int) (string, error)

type StoredSecrets struct {
	Password   string `json:"password,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}
type vaultPayload struct {
	Version   int                      `json:"version"`
	CreatedAt string                   `json:"created_at"`
	UpdatedAt string                   `json:"updated_at"`
	Secrets   map[string]StoredSecrets `json:"secrets"`
}
type vaultEnvelope struct {
	Version int `json:"version"`
	KDF     struct {
		Name      string `json:"name"`
		Salt      string `json:"salt"`
		N         int    `json:"N"`
		R         int    `json:"r"`
		P         int    `json:"p"`
		KeyLength int    `json:"key_length"`
	} `json:"kdf"`
	Cipher struct {
		Name    string `json:"name"`
		IV      string `json:"iv"`
		AuthTag string `json:"auth_tag"`
	} `json:"cipher"`
	Ciphertext string `json:"ciphertext"`
}
type VaultStatus struct {
	Path              string `json:"path"`
	Exists            bool   `json:"exists"`
	Unlocked          bool   `json:"unlocked"`
	SecretServerCount int    `json:"secret_server_count,omitempty"`
}

type CredentialVault struct {
	mu        sync.Mutex
	dir, path string
	prompt    SecretPrompt
	key, salt []byte
	payload   *vaultPayload
	N, R, P   int
}

func NewCredentialVault(dir string, prompt SecretPrompt) *CredentialVault {
	return &CredentialVault{dir: dir, path: filepath.Join(dir, SecretsFileName), prompt: prompt}
}

func (v *CredentialVault) SetPrompt(prompt SecretPrompt) {
	if v == nil || prompt == nil {
		return
	}
	v.mu.Lock()
	v.prompt = prompt
	v.mu.Unlock()
}

func (v *CredentialVault) Path() string { return v.path }
func (v *CredentialVault) Status() (VaultStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, err := os.Stat(v.path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return VaultStatus{}, err
	}
	count := 0
	if v.payload != nil {
		count = len(v.payload.Secrets)
	}
	return VaultStatus{Path: v.path, Exists: exists, Unlocked: v.payload != nil && len(v.key) == keyLength, SecretServerCount: count}, nil
}
func (v *CredentialVault) Unlock(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.unlockWithPrompt(ctx, true, "usar la bóveda SSH")
}

func (v *CredentialVault) Lock() { v.mu.Lock(); defer v.mu.Unlock(); v.clear() }
func (v *CredentialVault) Get(ctx context.Context, id string) (StoredSecrets, bool, error) {
	return v.get(ctx, id, "leer una credencial SSH guardada")
}

func (v *CredentialVault) GetForConnection(ctx context.Context, id, displayName string) (StoredSecrets, bool, error) {
	reason := "abrir o reparar la conexión remota"
	if displayName != "" {
		reason += " con " + displayName
	}
	return v.get(ctx, id, reason)
}

func (v *CredentialVault) get(ctx context.Context, id, reason string) (StoredSecrets, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(ctx, false, reason); err != nil {
		return StoredSecrets{}, false, err
	}
	s, ok := v.payload.Secrets[id]
	return s, ok, nil
}
func (v *CredentialVault) Set(ctx context.Context, id string, password, passphrase *string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(ctx, true, "guardar una credencial SSH"); err != nil {
		return err
	}
	current := v.payload.Secrets[id]
	if password != nil {
		current.Password = *password
	}
	if passphrase != nil {
		current.Passphrase = *passphrase
	}
	current.UpdatedAt = nowISO()
	if current.Password == "" && current.Passphrase == "" {
		delete(v.payload.Secrets, id)
	} else {
		v.payload.Secrets[id] = current
	}
	v.payload.UpdatedAt = nowISO()
	return v.writeUnlocked()
}

// EnsureWritable opens an existing vault or creates it before another secret
// is requested. This makes the UI sequence explicit: vault master password
// first (only when necessary), then the remote password or key passphrase.
func (v *CredentialVault) EnsureWritable(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.requireUnlocked(ctx, true, "guardar una credencial SSH")
}
func (v *CredentialVault) ClearField(ctx context.Context, id, field string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := os.Stat(v.path); errors.Is(err, os.ErrNotExist) && v.payload == nil {
		return false, nil
	}
	if err := v.requireUnlocked(ctx, false, "modificar las credenciales SSH guardadas"); err != nil {
		return false, err
	}
	s, ok := v.payload.Secrets[id]
	if !ok {
		return false, nil
	}
	changed := false
	switch field {
	case "password":
		changed = s.Password != ""
		s.Password = ""
	case "passphrase":
		changed = s.Passphrase != ""
		s.Passphrase = ""
	}
	if !changed {
		return false, nil
	}
	if s.Password == "" && s.Passphrase == "" {
		delete(v.payload.Secrets, id)
	} else {
		s.UpdatedAt = nowISO()
		v.payload.Secrets[id] = s
	}
	v.payload.UpdatedAt = nowISO()
	return true, v.writeUnlocked()
}
func (v *CredentialVault) DeleteServer(ctx context.Context, id string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := os.Stat(v.path); errors.Is(err, os.ErrNotExist) && v.payload == nil {
		return false, nil
	}
	if err := v.requireUnlocked(ctx, false, "modificar las credenciales SSH guardadas"); err != nil {
		return false, err
	}
	if _, ok := v.payload.Secrets[id]; !ok {
		return false, nil
	}
	delete(v.payload.Secrets, id)
	v.payload.UpdatedAt = nowISO()
	return true, v.writeUnlocked()
}
func (v *CredentialVault) ChangePassword(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(ctx, false, "cambiar la contraseña maestra de la bóveda"); err != nil {
		return err
	}
	password, err := v.ask(ctx, interaction.SecretVaultMaster, "Nueva contraseña maestra de la bóveda SSH", "Crea una nueva contraseña maestra para cifrar la bóveda local. No escribas la contraseña del servidor remoto. Deberás repetirla para confirmar.", true, 8)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return err
	}
	key, err := deriveVaultKey(password, salt, scryptN, scryptR, scryptP)
	if err != nil {
		return err
	}
	oldKey, oldSalt, oldN, oldR, oldP := v.key, v.salt, v.N, v.R, v.P
	v.key, v.salt, v.N, v.R, v.P = key, salt, scryptN, scryptR, scryptP
	if err = v.writeUnlocked(); err != nil {
		zero(key)
		v.key, v.salt, v.N, v.R, v.P = oldKey, oldSalt, oldN, oldR, oldP
		return err
	}
	zero(oldKey)
	return nil
}
func (v *CredentialVault) requireUnlocked(ctx context.Context, create bool, reason string) error {
	if v.payload != nil && len(v.key) == keyLength {
		return nil
	}
	_, err := os.Stat(v.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !create {
			return errors.New("la bóveda SSH todavía no existe")
		}
		return v.createVault(ctx)
	}
	// La bóveda se abre sólo cuando una operación realmente necesita una
	// credencial cifrada. Después permanece abierta en memoria hasta lock_vault
	// o hasta cerrar Lilith.
	return v.unlockWithPrompt(ctx, false, reason)
}

func (v *CredentialVault) unlockWithPrompt(ctx context.Context, create bool, reason string) error {
	if v.payload != nil && len(v.key) == keyLength {
		return nil
	}
	data, err := os.ReadFile(v.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return errors.New("la bóveda SSH todavía no existe")
		}
		return v.createVault(ctx)
	}
	var env vaultEnvelope
	if err = json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("la bóveda SSH está dañada: %w", err)
	}
	if err = validateEnvelope(env); err != nil {
		return err
	}
	salt, err := base64.StdEncoding.DecodeString(env.KDF.Salt)
	if err != nil {
		return errors.New("salt inválido en la bóveda SSH")
	}
	var last error
	for attempt := 1; attempt <= maxUnlockAttempts; attempt++ {
		message := "Lilith necesita desbloquear la bóveda local para " + reason + ". Escribe la contraseña maestra que creaste para la bóveda. No es la contraseña del servidor remoto. La bóveda permanecerá abierta sólo durante esta ejecución."
		if attempt > 1 {
			message = "La contraseña maestra anterior no pudo desbloquear la bóveda. Vuelve a escribir la contraseña de la bóveda; no la contraseña del servidor remoto."
		}
		password, e := v.ask(ctx, interaction.SecretVaultMaster, "Contraseña maestra de la bóveda SSH", message, false, 1)
		if e != nil {
			return e
		}
		key, e := deriveVaultKey(password, salt, env.KDF.N, env.KDF.R, env.KDF.P)
		if e != nil {
			return e
		}
		payload, e := decryptVault(env, key)
		if e == nil {
			v.key, v.salt, v.N, v.R, v.P = key, salt, env.KDF.N, env.KDF.R, env.KDF.P
			v.payload = payload
			return nil
		}
		zero(key)
		last = e
	}
	return fmt.Errorf("no se pudo desbloquear la bóveda SSH después de %d intentos: %v", maxUnlockAttempts, last)
}

func (v *CredentialVault) createVault(ctx context.Context) error {
	password, err := v.ask(ctx, interaction.SecretVaultMaster, "Crear contraseña maestra de la bóveda SSH", "Estás creando la contraseña maestra de la bóveda local. No es la contraseña del servidor remoto. Esta contraseña cifrará todas las credenciales SSH guardadas y se pedirá sólo cuando una operación necesite abrir la bóveda en una nueva ejecución.", true, 8)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return err
	}
	key, err := deriveVaultKey(password, salt, scryptN, scryptR, scryptP)
	if err != nil {
		return err
	}
	now := nowISO()
	v.key, v.salt, v.N, v.R, v.P = key, salt, scryptN, scryptR, scryptP
	v.payload = &vaultPayload{Version: payloadVersion, CreatedAt: now, UpdatedAt: now, Secrets: map[string]StoredSecrets{}}
	if err = v.writeUnlocked(); err != nil {
		v.clear()
		return err
	}
	return nil
}

func (v *CredentialVault) ask(ctx context.Context, secretKind interaction.SecretKind, title, message string, confirm bool, min int) (string, error) {
	if v.prompt == nil {
		return "", errors.New("Lilith necesita entrada secreta local para usar la bóveda SSH")
	}
	return v.prompt(ctx, secretKind, title, message, confirm, min)
}
func deriveVaultKey(password string, salt []byte, N, R, P int) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, N, R, P, keyLength)
}
func validateEnvelope(e vaultEnvelope) error {
	if e.Version != envelopeVersion {
		return fmt.Errorf("versión de bóveda SSH no compatible: %d", e.Version)
	}
	if e.KDF.Name != "scrypt" || e.Cipher.Name != "aes-256-gcm" {
		return errors.New("algoritmos de bóveda SSH no compatibles")
	}
	if e.KDF.N < 16384 || e.KDF.N > 262144 || e.KDF.N&(e.KDF.N-1) != 0 || e.KDF.R < 1 || e.KDF.R > 32 || e.KDF.P < 1 || e.KDF.P > 8 || e.KDF.KeyLength != keyLength {
		return errors.New("parámetros criptográficos de bóveda inválidos")
	}
	iv, _ := base64.StdEncoding.DecodeString(e.Cipher.IV)
	tag, _ := base64.StdEncoding.DecodeString(e.Cipher.AuthTag)
	if len(iv) != 12 || len(tag) != 16 {
		return errors.New("parámetros AES-GCM inválidos")
	}
	return nil
}
func decryptVault(e vaultEnvelope, key []byte) (*vaultPayload, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	iv, err := base64.StdEncoding.DecodeString(e.Cipher.IV)
	if err != nil {
		return nil, err
	}
	tag, err := base64.StdEncoding.DecodeString(e.Cipher.AuthTag)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err != nil {
		return nil, err
	}
	ct = append(ct, tag...)
	plain, err := gcm.Open(nil, iv, ct, vaultAAD)
	if err != nil {
		return nil, err
	}
	defer zero(plain)
	var p vaultPayload
	if err = json.Unmarshal(plain, &p); err != nil {
		return nil, err
	}
	if p.Version != payloadVersion || p.Secrets == nil {
		return nil, errors.New("contenido descifrado de bóveda no válido")
	}
	return &p, nil
}
func (v *CredentialVault) writeUnlocked() error {
	if v.payload == nil || len(v.key) != keyLength {
		return errors.New("la bóveda SSH no está desbloqueada")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(iv); err != nil {
		return err
	}
	plain, err := json.Marshal(v.payload)
	if err != nil {
		return err
	}
	defer zero(plain)
	sealed := gcm.Seal(nil, iv, plain, vaultAAD)
	tagSize := gcm.Overhead()
	ct, tag := sealed[:len(sealed)-tagSize], sealed[len(sealed)-tagSize:]
	var env vaultEnvelope
	env.Version = envelopeVersion
	env.KDF.Name = "scrypt"
	env.KDF.Salt = base64.StdEncoding.EncodeToString(v.salt)
	env.KDF.N, env.KDF.R, env.KDF.P, env.KDF.KeyLength = v.N, v.R, v.P, keyLength
	env.Cipher.Name = "aes-256-gcm"
	env.Cipher.IV = base64.StdEncoding.EncodeToString(iv)
	env.Cipher.AuthTag = base64.StdEncoding.EncodeToString(tag)
	env.Ciphertext = base64.StdEncoding.EncodeToString(ct)
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(v.path, append(data, '\n'), 0600)
}
func (v *CredentialVault) clear() {
	zero(v.key)
	zero(v.salt)
	v.key = nil
	v.salt = nil
	v.payload = nil
	v.N, v.R, v.P = 0, 0, 0
}
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

var _ = time.Now
