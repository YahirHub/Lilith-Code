package sshremote

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/lilith/li/internal/interaction"
)

type promptRecord struct {
	kind           interaction.SecretKind
	title, message string
	confirm        bool
	minLength      int
}

type promptQueue struct {
	mu       sync.Mutex
	values   []string
	requests []promptRecord
}

func (p *promptQueue) prompt(_ context.Context, kind interaction.SecretKind, title, message string, confirm bool, minLength int) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, promptRecord{kind: kind, title: title, message: message, confirm: confirm, minLength: minLength})
	if len(p.values) == 0 {
		return "", context.Canceled
	}
	v := p.values[0]
	p.values = p.values[1:]
	return v, nil
}

func (p *promptQueue) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *promptQueue) lastRequest() promptRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return promptRecord{}
	}
	return p.requests[len(p.requests)-1]
}

func TestCredentialVaultEncryptsLocksAndReopens(t *testing.T) {
	dir := t.TempDir()
	q := &promptQueue{values: []string{"maestra-segura"}}
	vault := NewCredentialVault(dir, q.prompt)
	password := "servidor-secreto"
	passphrase := "clave-secreta"
	if err := vault.Set(context.Background(), "server-1", &password, &passphrase); err != nil {
		t.Fatal(err)
	}
	status, err := vault.Status()
	if err != nil || !status.Exists || !status.Unlocked || status.SecretServerCount != 1 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	data, err := os.ReadFile(vault.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{password, passphrase, "maestra-segura"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("la bóveda expuso %q", forbidden)
		}
	}
	if !strings.Contains(string(data), "aes-256-gcm") || !strings.Contains(string(data), "scrypt") {
		t.Fatalf("envoltura inesperada: %s", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(vault.Path())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permisos=%o", info.Mode().Perm())
		}
	}
	vault.Lock()
	status, _ = vault.Status()
	if status.Unlocked {
		t.Fatal("Lock debía eliminar la clave en memoria")
	}
	q.values = []string{"incorrecta", "maestra-segura"}
	if err := vault.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := vault.Get(context.Background(), "server-1")
	if err != nil || !ok || got.Password != password || got.Passphrase != passphrase {
		t.Fatalf("secret=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestCredentialVaultChangesMasterPassword(t *testing.T) {
	dir := t.TempDir()
	q := &promptQueue{values: []string{"primera-maestra"}}
	vault := NewCredentialVault(dir, q.prompt)
	secret := "password"
	if err := vault.Set(context.Background(), "server", &secret, nil); err != nil {
		t.Fatal(err)
	}
	q.values = []string{"segunda-maestra"}
	if err := vault.ChangePassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	vault.Lock()
	q.values = []string{"primera-maestra", "segunda-maestra"}
	if err := vault.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := vault.Get(context.Background(), "server")
	if err != nil || !ok || got.Password != secret {
		t.Fatalf("secret=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestCredentialVaultUnlocksLazilyForSavedConnectionAndStaysOpen(t *testing.T) {
	dir := t.TempDir()
	q := &promptQueue{values: []string{"maestra-segura"}}
	vault := NewCredentialVault(dir, q.prompt)
	secret := "password"
	if err := vault.Set(context.Background(), "server", &secret, nil); err != nil {
		t.Fatal(err)
	}
	vault.Lock()
	before := q.requestCount()
	q.mu.Lock()
	q.values = []string{"maestra-segura"}
	q.mu.Unlock()

	got, ok, err := vault.GetForConnection(context.Background(), "server", "producción")
	if err != nil || !ok || got.Password != secret {
		t.Fatalf("secret=%#v ok=%v err=%v", got, ok, err)
	}
	if q.requestCount() != before+1 {
		t.Fatalf("lazy unlock prompts=%d, want %d", q.requestCount(), before+1)
	}
	req := q.lastRequest()
	combined := strings.ToLower(req.title + " " + req.message)
	for _, want := range []string{"contraseña maestra", "bóveda", "no es la contraseña del servidor", "producción"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("prompt de desbloqueo no explica %q: %#v", want, req)
		}
	}

	// La misma instancia queda abierta en memoria: las tareas SSH siguientes no
	// deben volver a pedir la contraseña maestra.
	count := q.requestCount()
	got, ok, err = vault.GetForConnection(context.Background(), "server", "producción")
	if err != nil || !ok || got.Password != secret {
		t.Fatalf("second get secret=%#v ok=%v err=%v", got, ok, err)
	}
	if q.requestCount() != count {
		t.Fatalf("la bóveda abierta volvió a mostrar el prompt: got=%d want=%d", q.requestCount(), count)
	}
}

func TestCredentialVaultCreationPromptDistinguishesMasterFromServerPassword(t *testing.T) {
	dir := t.TempDir()
	q := &promptQueue{values: []string{"maestra-segura"}}
	vault := NewCredentialVault(dir, q.prompt)
	secret := "password"
	if err := vault.Set(context.Background(), "server", &secret, nil); err != nil {
		t.Fatal(err)
	}
	req := q.lastRequest()
	combined := strings.ToLower(req.title + " " + req.message)
	for _, want := range []string{"crear", "contraseña maestra", "bóveda", "no es la contraseña del servidor remoto"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("prompt de creación no explica %q: %#v", want, req)
		}
	}
	if !req.confirm {
		t.Fatal("la creación de la contraseña maestra debe pedir confirmación")
	}
	if req.kind != interaction.SecretVaultMaster {
		t.Fatalf("kind=%q", req.kind)
	}
}

func TestCredentialVaultStaysOpenWhileSavingMoreCredentials(t *testing.T) {
	dir := t.TempDir()
	q := &promptQueue{values: []string{"maestra-segura"}}
	vault := NewCredentialVault(dir, q.prompt)
	if err := vault.EnsureWritable(context.Background()); err != nil {
		t.Fatal(err)
	}
	prompts := q.requestCount()
	password := "servidor-uno"
	if err := vault.Set(context.Background(), "server-1", &password, nil); err != nil {
		t.Fatal(err)
	}
	password = "servidor-dos"
	if err := vault.Set(context.Background(), "server-2", &password, nil); err != nil {
		t.Fatal(err)
	}
	if got := q.requestCount(); got != prompts {
		t.Fatalf("la bóveda abierta volvió a pedir la maestra: got=%d want=%d", got, prompts)
	}
}
