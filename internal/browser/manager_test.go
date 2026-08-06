package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestRunInitialKeepsPersistentContextAliveAfterSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := func(runCtx context.Context, _ ...chromedp.Action) error {
		if runCtx != ctx {
			t.Fatal("runInitial creó o sustituyó el contexto persistente")
		}
		return nil
	}
	if err := runInitialWith(ctx, cancel, time.Second, runner); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("el contexto persistente quedó cancelado después del arranque: %v", err)
	}
}

func TestRunInitialCancelsPersistentContextOnTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := func(runCtx context.Context, _ ...chromedp.Action) error {
		<-runCtx.Done()
		return runCtx.Err()
	}
	err := runInitialWith(ctx, cancel, 5*time.Millisecond, runner)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("se esperaba deadline exceeded, se obtuvo %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("el contexto persistente no fue cancelado tras el timeout: %v", ctx.Err())
	}
}

func TestOperationContextCancellationDoesNotClosePersistentContext(t *testing.T) {
	persistent, stopPersistent := context.WithCancel(context.Background())
	defer stopPersistent()
	request, stopRequest := context.WithCancel(context.Background())
	opCtx, stopOperation := operationContext(request, persistent, time.Second)
	defer stopOperation()

	stopRequest()
	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("la cancelación de la solicitud no alcanzó a la operación CDP")
	}
	if err := persistent.Err(); err != nil {
		t.Fatalf("cancelar una operación cerró el contexto persistente: %v", err)
	}
}

func TestEnableDebuggerImplementsAction(t *testing.T) {
	var action chromedp.Action = enableDebugger()
	if action == nil {
		t.Fatal("enableDebugger devolvió una acción nula")
	}
}

func TestStartDoesNotPassUnsupportedExecAllocatorFlags(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "fake-browser")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("not a browser"), 0o755); err != nil {
		t.Fatal(err)
	}

	manager := &Manager{configDir: t.TempDir(), sessions: map[string]*Session{}}
	_, err := manager.Start(context.Background(), StartOptions{
		SessionID:   "allocator-flags",
		Executable:  executable,
		ProfileMode: ProfileTemporary,
	})
	if err == nil {
		t.Fatal("se esperaba un error al intentar ejecutar un archivo que no es navegador")
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid exec pool flag") {
		t.Fatalf("Chromedp recibió un valor de flag no compatible: %v", err)
	}
}

func TestValidateSessionID(t *testing.T) {
	for _, value := range []string{"jsecure", "panel-pruebas", "browser_01", "qa.local"} {
		if err := validateSessionID(value); err != nil {
			t.Fatalf("session_id válido %q fue rechazado: %v", value, err)
		}
	}
	for _, value := range []string{"con espacios", "ruta/otra", "comando;rm", strings.Repeat("a", 81)} {
		if err := validateSessionID(value); err == nil {
			t.Fatalf("session_id inválido %q fue aceptado", value)
		}
	}
}

func TestBrowserIntegrationStartAndSnapshot(t *testing.T) {
	if os.Getenv("LILITH_BROWSER_INTEGRATION") != "1" {
		t.Skip("define LILITH_BROWSER_INTEGRATION=1 para ejecutar la prueba con un navegador real")
	}
	executable := strings.TrimSpace(os.Getenv("LILITH_BROWSER_EXECUTABLE"))
	if executable == "" {
		candidates, err := Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range candidates {
			if candidate.Executable != "" {
				executable = candidate.Executable
				break
			}
		}
	}
	if executable == "" {
		t.Fatal("no se encontró un navegador para la prueba de integración")
	}

	manager := &Manager{configDir: t.TempDir(), sessions: map[string]*Session{}}
	info, err := manager.Start(context.Background(), StartOptions{
		SessionID:   "integration",
		Executable:  executable,
		Headless:    true,
		ProfileMode: ProfileTemporary,
		StartURL:    "data:text/html,<title>Lilith Browser Test</title><main><button id='ready'>Listo</button></main>",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.CloseAll()
	if info.ID != "integration" {
		t.Fatalf("session_id inesperado: %q", info.ID)
	}

	session, err := manager.Session(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Info().Attached {
		t.Fatal("la sesión CDP figura desconectada inmediatamente después de start")
	}
	if err := session.Navigate(context.Background(), "data:text/html,<title>Lilith Persistent CDP</title><main><button id='ready'>Listo</button></main>"); err != nil {
		t.Fatalf("navigate posterior a start falló: %v", err)
	}
	snapshot, err := session.Snapshot(context.Background(), false, 2000, 20)
	if err != nil {
		t.Fatalf("snapshot posterior a navigate falló: %v", err)
	}
	if snapshot.Title != "Lilith Persistent CDP" {
		t.Fatalf("título inesperado: %q", snapshot.Title)
	}
	if len(snapshot.Elements) == 0 || snapshot.Elements[0].Text != "Listo" {
		t.Fatalf("snapshot no contiene el botón esperado: %#v", snapshot.Elements)
	}
	tabs, err := session.Tabs(context.Background())
	if err != nil {
		t.Fatalf("status/tabs posterior a snapshot falló: %v", err)
	}
	if len(tabs) == 0 {
		t.Fatal("status devolvió una lista de pestañas vacía")
	}
	screenshot := filepath.Join(t.TempDir(), "persistent.png")
	if _, err := session.Screenshot(context.Background(), "", screenshot, false, 0); err != nil {
		t.Fatalf("screenshot posterior a varias acciones falló: %v", err)
	}
	if info, err := os.Stat(screenshot); err != nil || info.Size() == 0 {
		t.Fatalf("screenshot no fue generado correctamente: info=%v err=%v", info, err)
	}
}
