package interaction

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBridgeSecretNeverLeavesLocalRequest(t *testing.T) {
	b := NewBridge()
	defer b.Close()
	result := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		v, err := b.RequestSecret(context.Background(), "Bóveda", "Contraseña local", true, 8)
		if err != nil {
			errCh <- err
			return
		}
		result <- v
	}()
	req, ok := b.Next()
	if !ok || req == nil || req.Kind != Secret || !req.Confirm || req.MinLength != 8 {
		t.Fatalf("solicitud inesperada: %#v ok=%v", req, ok)
	}
	if strings.Contains(req.Message, "clave-super-secreta") {
		t.Fatal("la solicitud no debe contener el secreto")
	}
	req.Resolve(Result{Value: "clave-super-secreta", Approved: true})
	select {
	case err := <-errCh:
		t.Fatal(err)
	case got := <-result:
		if got != "clave-super-secreta" {
			t.Fatalf("secreto inesperado: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("la solicitud no terminó")
	}
}

func TestBridgeCloseUnblocksPendingRequest(t *testing.T) {
	b := NewBridge()
	errCh := make(chan error, 1)
	go func() {
		_, err := b.RequestConfirm(context.Background(), "SSH", "Autorizar")
		errCh <- err
	}()
	if _, ok := b.Next(); !ok {
		t.Fatal("se esperaba una solicitud")
	}
	b.Close()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "cerrando") {
			t.Fatalf("error inesperado: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close no desbloqueó al solicitante")
	}
}

func TestBridgeSessionApprovalSkipsRepeatedPrompt(t *testing.T) {
	b := NewBridge()
	defer b.Close()
	first := make(chan ApprovalDecision, 1)
	go func() {
		decision, _ := b.RequestApproval(context.Background(), "SSH", "Ejecutar uname", "ssh|project|exec|server")
		first <- decision
	}()
	req, ok := b.Next()
	if !ok || req == nil || !req.AllowScope {
		t.Fatalf("approval request inesperada: %#v", req)
	}
	req.Resolve(Result{Approved: true, Decision: ApprovalSession})
	if got := <-first; got != ApprovalSession {
		t.Fatalf("decision=%q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	decision, err := b.RequestApproval(ctx, "SSH", "Ejecutar uname", "ssh|project|exec|server")
	if err != nil || decision != ApprovalSession {
		t.Fatalf("session approval=%q err=%v", decision, err)
	}
}
