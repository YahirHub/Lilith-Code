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
