package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lilith/li/internal/interaction"
	"github.com/lilith/li/internal/tui/uikit"
)

func TestConfirmRequestRendersInsideChatAndApproves(t *testing.T) {
	chat := newInputTestChat(t)
	bridge := interaction.NewBridge()
	defer bridge.Close()
	chat.ctx.Interactions = bridge
	root := RootModel{ctx: chat.ctx, chat: chat, current: chat}

	result := make(chan bool, 1)
	go func() {
		ok, err := bridge.RequestConfirm(context.Background(), "Permiso SSH remoto", "Ejecutar comando remoto\nDestino: uname -a")
		result <- ok && err == nil
	}()
	req, ok := bridge.Next()
	if !ok || req == nil {
		t.Fatal("confirmation request was not queued")
	}

	next, _ := root.Update(interactionRequestMsg{request: req})
	root = next.(RootModel)
	if root.current != chat || chat.permissionRequest != req {
		t.Fatal("confirmation must stay in the persistent chat")
	}
	view := stripANSI(root.View())
	for _, want := range []string{"Permiso SSH remoto", "Ejecutar comando remoto", "Permitir una vez", "Denegar"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission widget missing %q:\n%s", want, view)
		}
	}

	_, cmd := chat.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	if cmd == nil {
		t.Fatal("approval did not produce a resolution command")
	}
	out := cmd()
	batch, ok := out.(uikit.BatchMsg)
	if !ok {
		t.Fatalf("approval command=%T, want BatchMsg", out)
	}
	resolved := false
	for _, child := range batch {
		if child == nil {
			continue
		}
		msg := child()
		resolution, ok := msg.(interactionResolvedMsg)
		if !ok {
			continue
		}
		resolved = true
		next, _ = root.Update(resolution)
		root = next.(RootModel)
	}
	if !resolved {
		t.Fatal("permission widget did not emit interactionResolvedMsg")
	}
	select {
	case approved := <-result:
		if !approved {
			t.Fatal("bridge did not receive approval")
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not unblock the waiting tool")
	}
	if chat.permissionRequest != nil {
		t.Fatal("permission widget remained open after approval")
	}
}

func TestConfirmRequestEscDenies(t *testing.T) {
	chat := newInputTestChat(t)
	req := &interaction.Request{Kind: interaction.Confirm, Title: "SSH", Message: "Borrar archivo"}
	chat.openPermission(req)
	_, cmd := chat.Update(uikit.KeyMsg{Type: uikit.KeyEsc})
	if cmd == nil || chat.permissionRequest != nil {
		t.Fatal("Esc must close and deny the permission request")
	}
}

func TestConfirmWidgetRestoresPreviousScreen(t *testing.T) {
	chat := newInputTestChat(t)
	previous := NewConfigScreen(chat.ctx)
	req := &interaction.Request{Kind: interaction.Confirm, Title: "SSH", Message: "Autorizar"}
	root := RootModel{ctx: chat.ctx, chat: chat, current: previous}

	next, _ := root.Update(interactionRequestMsg{request: req})
	root = next.(RootModel)
	if root.current != chat || root.interactionPrevious != previous {
		t.Fatal("permission widget did not preserve the previous screen")
	}

	next, _ = root.Update(interactionResolvedMsg{request: req, result: interaction.Result{Approved: true}})
	root = next.(RootModel)
	if root.current != previous || root.interactionPrevious != nil {
		t.Fatal("permission widget did not restore the previous screen")
	}
}

func TestVaultSecretRequestRendersAsMaskedChatPopup(t *testing.T) {
	chat := newInputTestChat(t)
	bridge := interaction.NewBridge()
	defer bridge.Close()
	chat.ctx.Interactions = bridge
	root := RootModel{ctx: chat.ctx, chat: chat, current: chat}

	result := make(chan string, 1)
	go func() {
		value, err := bridge.RequestSecretKind(
			context.Background(),
			interaction.SecretVaultMaster,
			"Contraseña maestra de la bóveda SSH",
			"Lilith necesita abrir la bóveda. No es la contraseña del servidor remoto.",
			false,
			1,
		)
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- value
	}()
	req, ok := bridge.Next()
	if !ok || req == nil {
		t.Fatal("secret request was not queued")
	}

	next, _ := root.Update(interactionRequestMsg{request: req})
	root = next.(RootModel)
	if root.current != chat || chat.permissionRequest != req || chat.secretPrompt == nil {
		t.Fatal("secret prompt must stay inside the persistent chat")
	}
	view := stripANSI(root.View())
	for _, want := range []string{"Contraseña maestra de la bóveda SSH", "No es la contraseña del servidor remoto", "Entrada local protegida"} {
		if !strings.Contains(view, want) {
			t.Fatalf("secret popup missing %q:\n%s", want, view)
		}
	}

	secret := "maestra-segura"
	_, _ = chat.Update(uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune(secret)})
	view = stripANSI(root.View())
	if strings.Contains(view, secret) {
		t.Fatalf("secret appeared unmasked in the chat popup:\n%s", view)
	}
	if !strings.Contains(view, "••••") {
		t.Fatalf("masked secret was not rendered:\n%s", view)
	}

	_, cmd := chat.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	if cmd == nil {
		t.Fatal("secret submission did not produce a resolution command")
	}
	out := cmd()
	batch, ok := out.(uikit.BatchMsg)
	if !ok {
		t.Fatalf("secret command=%T, want BatchMsg", out)
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		msg := child()
		resolution, ok := msg.(interactionResolvedMsg)
		if !ok {
			continue
		}
		next, _ = root.Update(resolution)
		root = next.(RootModel)
	}
	select {
	case got := <-result:
		if got != secret {
			t.Fatalf("bridge secret=%q want=%q", got, secret)
		}
	case <-time.After(time.Second):
		t.Fatal("secret prompt did not unblock the waiting operation")
	}
	if chat.permissionRequest != nil || chat.secretPrompt != nil {
		t.Fatal("secret popup remained open after submission")
	}
}

func TestServerPasswordPopupIsExplicit(t *testing.T) {
	chat := newInputTestChat(t)
	req := &interaction.Request{
		Kind:       interaction.Secret,
		SecretKind: interaction.SecretRemotePassword,
		Title:      "Contraseña del servidor remoto",
		Message:    "Escribe la contraseña de user@example.com. No es la contraseña maestra de la bóveda SSH.",
		MinLength:  1,
	}
	chat.openPermission(req)
	view := stripANSI(chat.permissionDockView(chat.ctx.Width))
	for _, want := range []string{"Contraseña del servidor remoto", "No es la contraseña maestra de la bóveda SSH"} {
		if !strings.Contains(view, want) {
			t.Fatalf("server password popup missing %q:\n%s", want, view)
		}
	}
}

func TestScopedSSHPermissionRendersAllDecisions(t *testing.T) {
	chat := newInputTestChat(t)
	req := &interaction.Request{Kind: interaction.Confirm, Title: "Permiso SSH remoto", Message: "Ejecutar comando", AllowScope: true}
	chat.openPermission(req)
	view := stripANSI(chat.permissionDockView(chat.ctx.Width))
	for _, want := range []string{"Permitir una vez", "Permitir en esta sesión", "Permitir siempre en este proyecto", "Denegar"} {
		if !strings.Contains(view, want) {
			t.Fatalf("falta opción %q:\n%s", want, view)
		}
	}
}
