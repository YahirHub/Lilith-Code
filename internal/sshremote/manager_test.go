package sshremote

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/lilith/li/internal/interaction"
	"golang.org/x/crypto/ssh"
)

func TestManagerRenamesAndDetachesActiveServerConnections(t *testing.T) {
	m := &Manager{connections: map[string]*Connection{
		"one": {ID: "one", ServerID: "server-1", DisplayName: "old", shells: map[string]*RemoteShell{}},
		"two": {ID: "two", ServerID: "server-2", DisplayName: "other", shells: map[string]*RemoteShell{}},
	}}
	if got := m.RenameServerConnections("server-1", "new"); got != 1 {
		t.Fatalf("renamed=%d", got)
	}
	if m.connections["one"].DisplayName != "new" || m.connections["two"].DisplayName != "other" {
		t.Fatalf("nombres inesperados: %#v %#v", m.connections["one"], m.connections["two"])
	}
	if got := m.DetachServer("server-1"); got != 1 {
		t.Fatalf("detached=%d", got)
	}
	if m.connections["one"].ServerID != "" {
		t.Fatalf("server id no fue separado: %q", m.connections["one"].ServerID)
	}
}

func TestShutdownAllClosesConnectionsAndLocksVault(t *testing.T) {
	ShutdownAll()
	dir := t.TempDir()
	q := &promptQueue{values: []string{"maestra-segura"}}
	m := GetManager(dir, q.prompt, nil)
	secret := "password"
	if err := m.Vault().Set(context.Background(), "server", &secret, nil); err != nil {
		t.Fatal(err)
	}
	m.connections["conn"] = &Connection{ID: "conn", shells: map[string]*RemoteShell{}}
	ShutdownAll()
	status, err := m.Vault().Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Unlocked {
		t.Fatal("la bóveda debía quedar bloqueada al cerrar el CLI")
	}
	if len(m.connections) != 0 {
		t.Fatalf("conexiones restantes=%d", len(m.connections))
	}
}

func TestBoundedBufferReportsTruncationWithoutShortWrite(t *testing.T) {
	buffer := boundedBuffer{max: 4}
	input := []byte("abcdef")
	n, err := buffer.Write(input)
	if err != nil || n != len(input) {
		t.Fatalf("write n=%d err=%v", n, err)
	}
	if buffer.String() != "abcd" || !buffer.truncated {
		t.Fatalf("buffer=%q truncated=%v", buffer.String(), buffer.truncated)
	}
}

func TestRemoteShellReadSeparatesStreamsAndConsumesPendingOutput(t *testing.T) {
	shell := &RemoteShell{
		ID: "shell-1", done: make(chan struct{}), stdin: nopWriteCloser{}, session: nil,
		stdout: []byte("stdout"), stderr: []byte("stderr"), buffer: []byte("combined"),
	}
	out := shell.Read(4)
	if out["stdout"] != "stdo" || out["stderr"] != "stde" || out["output"] != "comb" {
		t.Fatalf("salida inesperada: %#v", out)
	}
	if out["stdout_truncated"] != true || out["stderr_truncated"] != true || out["output_truncated"] != true {
		t.Fatalf("flags inesperados: %#v", out)
	}
	next := shell.Read(100)
	if next["stdout"] != "" || next["stderr"] != "" || next["output"] != "" {
		t.Fatalf("la salida no se consumió: %#v", next)
	}
}

func TestPromptedServerPasswordStaysInLogicalConnectionMemory(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	calls := 0
	m := &Manager{prompt: func(_ context.Context, kind interaction.SecretKind, title, message string, confirm bool, minLength int) (string, error) {
		calls++
		if kind != interaction.SecretRemotePassword {
			t.Fatalf("tipo de secreto=%q", kind)
		}
		if title != "Contraseña del servidor remoto" {
			t.Fatalf("título ambiguo: %q", title)
		}
		if confirm || minLength != 1 {
			t.Fatalf("parámetros inesperados: confirm=%v min=%d", confirm, minLength)
		}
		return "server-secret", nil
	}}
	opt := ConnectOptions{Host: "example.invalid", Username: "deploy", PromptPassword: true}
	methods, agentConn, err := m.authMethods(context.Background(), &opt)
	if err != nil {
		t.Fatal(err)
	}
	if agentConn != nil {
		_ = agentConn.Close()
	}
	if len(methods) == 0 || opt.Password != "server-secret" {
		t.Fatalf("la credencial efímera no quedó disponible para reconectar: methods=%d password=%q", len(methods), opt.Password)
	}
	_, agentConn, err = m.authMethods(context.Background(), &opt)
	if agentConn != nil {
		_ = agentConn.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("el popup se pidió %d veces; debía reutilizarse sólo en memoria", calls)
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

func TestSSHTransportErrorsRecognizeEOFAndMissingExitStatus(t *testing.T) {
	for _, err := range []error{io.EOF, &ssh.ExitMissingError{}, errors.New("wait: remote command exited without exit status or exit signal"), errors.New("read: connection reset by peer")} {
		if !isSSHTransportError(err) {
			t.Fatalf("error %q was not recognized as transport failure", err)
		}
	}
	if isSSHTransportError(errors.New("Process exited with status 1")) {
		t.Fatal("a normal remote exit error must not force transport reconnection")
	}
}

func TestConnectionSnapshotPreservesStableLogicalIDAcrossGenerations(t *testing.T) {
	now := time.Now()
	c := &Connection{
		ID: "ssh-stable", DisplayName: "server", Generation: 3, ReconnectCount: 2,
		transportHealthy: true, LastReconnectAt: now, LastTransportErr: "EOF",
		shells: map[string]*RemoteShell{},
	}
	s := c.Snapshot()
	if s.ID != "ssh-stable" || s.Generation != 3 || s.ReconnectCount != 2 || !s.TransportHealthy {
		t.Fatalf("snapshot lost logical connection state: %#v", s)
	}
	if !s.LastReconnectAt.Equal(now) || s.LastTransportError != "EOF" {
		t.Fatalf("snapshot lost transport diagnostics: %#v", s)
	}
}

func TestStaleTransportFailureCannotInvalidateReplacementClient(t *testing.T) {
	current := &ssh.Client{}
	stale := &ssh.Client{}
	c := &Connection{client: current, transportHealthy: true, shells: map[string]*RemoteShell{}}
	c.markTransportFailedFor(stale, io.EOF)
	if !c.Snapshot().TransportHealthy {
		t.Fatal("a stale transport watcher invalidated the replacement client")
	}
	c.markTransportFailedFor(current, io.EOF)
	if c.Snapshot().TransportHealthy {
		t.Fatal("the current transport failure was not recorded")
	}
}
