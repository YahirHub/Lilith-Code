package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requiere shell POSIX")
	}
	res, err := Run(context.Background(), Request{Command: "echo hola; echo malo 1>&2; exit 3"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Stdout, "hola") {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "malo") {
		t.Errorf("stderr = %q", res.Stderr)
	}
	if res.ExitCode != 3 {
		t.Errorf("exitCode = %d, quiero 3", res.ExitCode)
	}
}

func TestRunTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requiere shell POSIX")
	}
	res, err := Run(context.Background(), Request{Command: "sleep 5", Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("esperaba timeout, obtuve %+v", res)
	}
}

func TestRunRejectsEmptyCommand(t *testing.T) {
	if _, err := Run(context.Background(), Request{Command: "   "}); err == nil {
		t.Fatal("esperaba error con comando vacío")
	}
}

func TestRunRejectsInvalidDir(t *testing.T) {
	if _, err := Run(context.Background(), Request{Command: "echo x", Dir: "/no/existe/lilith"}); err == nil {
		t.Fatal("esperaba error con directorio inválido")
	}
}

func TestRunCancelReturnsPromptly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requiere shell POSIX en el entorno de pruebas")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var res Result
	var runErr error
	go func() {
		defer close(done)
		res, runErr = Run(ctx, Request{Command: "sleep 30"})
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("la cancelación tardó demasiado: %s", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("el comando no terminó dentro de 500ms tras cancelar")
	}
	if runErr != nil {
		t.Fatalf("Run cancelado devolvió error Go: %v", runErr)
	}
	if !res.Canceled {
		t.Fatalf("esperaba Canceled=true, obtuvo %+v", res)
	}
}

func TestWithOptionalTimeoutOmittedHasNoDeadline(t *testing.T) {
	ctx, cancel := withOptionalTimeout(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("timeout omitido no debe crear una fecha límite")
	}
}

func TestWithOptionalTimeoutExplicitCreatesDeadline(t *testing.T) {
	ctx, cancel := withOptionalTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("timeout positivo debe crear una fecha límite")
	}
}
