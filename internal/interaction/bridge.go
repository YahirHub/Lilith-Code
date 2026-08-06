// Package interaction provides a process-local bridge between background tools
// and Lilith's terminal UI. Secrets travel only through this bridge: they are
// never serialized into tool arguments, chat history, logs or sessions.
package interaction

import (
	"context"
	"errors"
	"sync"
)

type Kind string

type SecretKind string

type ApprovalDecision string

const (
	Secret  Kind = "secret"
	Confirm Kind = "confirm"

	SecretGeneric        SecretKind = "generic"
	SecretVaultMaster    SecretKind = "vault_master"
	SecretRemotePassword SecretKind = "remote_password"
	SecretSudoPassword   SecretKind = "sudo_password"
	SecretKeyPassphrase  SecretKind = "key_passphrase"

	ApprovalDeny    ApprovalDecision = "deny"
	ApprovalOnce    ApprovalDecision = "allow_once"
	ApprovalSession ApprovalDecision = "allow_session"
	ApprovalProject ApprovalDecision = "allow_project"
)

type Request struct {
	Kind        Kind
	SecretKind  SecretKind
	Title       string
	Message     string
	ApprovalKey string
	AllowScope  bool
	Confirm     bool
	MinLength   int
	Attempt     int
	MaxAttempts int
	response    chan Result
	canceled    <-chan struct{}
}

type Result struct {
	Value    string
	Approved bool
	Canceled bool
	Decision ApprovalDecision
}

type Bridge struct {
	requests chan *Request
	closed   chan struct{}
	once     sync.Once
	mu       sync.RWMutex
	session  map[string]struct{}
}

func NewBridge() *Bridge {
	return &Bridge{requests: make(chan *Request, 16), closed: make(chan struct{}), session: map[string]struct{}{}}
}

func (b *Bridge) Close() {
	if b != nil {
		b.once.Do(func() { close(b.closed) })
	}
}

func (b *Bridge) Next() (*Request, bool) {
	if b == nil {
		return nil, false
	}
	select {
	case req := <-b.requests:
		return req, req != nil
	case <-b.closed:
		return nil, false
	}
}

func (r *Request) Canceled() bool {
	if r == nil || r.canceled == nil {
		return false
	}
	select {
	case <-r.canceled:
		return true
	default:
		return false
	}
}

func (r *Request) Resolve(result Result) {
	if r == nil {
		return
	}
	select {
	case r.response <- result:
	default:
	}
}

func (b *Bridge) RequestSecret(ctx context.Context, title, message string, confirm bool, minLength int) (string, error) {
	return b.RequestSecretKind(ctx, SecretGeneric, title, message, confirm, minLength)
}

func (b *Bridge) RequestSecretKind(ctx context.Context, secretKind SecretKind, title, message string, confirm bool, minLength int) (string, error) {
	if secretKind == "" {
		secretKind = SecretGeneric
	}
	req := &Request{Kind: Secret, SecretKind: secretKind, Title: title, Message: message, Confirm: confirm, MinLength: minLength, response: make(chan Result, 1), canceled: ctx.Done()}
	result, err := b.submit(ctx, req)
	if err != nil {
		return "", err
	}
	if result.Canceled {
		return "", errors.New("entrada secreta cancelada por el usuario")
	}
	if len(result.Value) < minLength {
		return "", errors.New("la contraseña no cumple la longitud mínima")
	}
	return result.Value, nil
}

func (b *Bridge) RequestConfirm(ctx context.Context, title, message string) (bool, error) {
	req := &Request{Kind: Confirm, Title: title, Message: message, response: make(chan Result, 1), canceled: ctx.Done()}
	result, err := b.submit(ctx, req)
	if err != nil {
		return false, err
	}
	return result.Approved && !result.Canceled, nil
}

// RequestApproval exposes the richer SSH permission widget. ApprovalKey is a
// process-local capability identity (for example one SSH action on one logical
// server). Selecting "allow for this session" remembers only that key until
// Lilith exits; project persistence is deliberately handled by the caller so
// the interaction package stays independent from settings storage.
func (b *Bridge) RequestApproval(ctx context.Context, title, message, approvalKey string) (ApprovalDecision, error) {
	approvalKey = normalizeApprovalKey(approvalKey)
	if approvalKey != "" && b.sessionAllowed(approvalKey) {
		return ApprovalSession, nil
	}
	req := &Request{
		Kind: Confirm, Title: title, Message: message,
		ApprovalKey: approvalKey, AllowScope: approvalKey != "",
		response: make(chan Result, 1), canceled: ctx.Done(),
	}
	result, err := b.submit(ctx, req)
	if err != nil {
		return ApprovalDeny, err
	}
	decision := result.Decision
	if decision == "" {
		if result.Approved && !result.Canceled {
			decision = ApprovalOnce
		} else {
			decision = ApprovalDeny
		}
	}
	if decision == ApprovalSession && approvalKey != "" {
		b.mu.Lock()
		b.session[approvalKey] = struct{}{}
		b.mu.Unlock()
	}
	return decision, nil
}

func (b *Bridge) sessionAllowed(key string) bool {
	b.mu.RLock()
	_, ok := b.session[key]
	b.mu.RUnlock()
	return ok
}

func normalizeApprovalKey(key string) string {
	// Keys are generated internally, but trimming here prevents accidental
	// duplicate session grants caused by surrounding whitespace.
	for len(key) > 0 && (key[0] == ' ' || key[0] == '\t' || key[0] == '\n' || key[0] == '\r') {
		key = key[1:]
	}
	for len(key) > 0 {
		last := key[len(key)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		key = key[:len(key)-1]
	}
	return key
}

func (b *Bridge) submit(ctx context.Context, req *Request) (Result, error) {
	if b == nil {
		return Result{}, errors.New("entrada interactiva no disponible")
	}
	select {
	case b.requests <- req:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-b.closed:
		return Result{}, errors.New("Lilith se está cerrando")
	}
	select {
	case result := <-req.response:
		return result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-b.closed:
		return Result{}, errors.New("Lilith se está cerrando")
	}
}
