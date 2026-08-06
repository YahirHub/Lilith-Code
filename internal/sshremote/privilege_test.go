package sshremote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePrivilegeMode(t *testing.T) {
	for input, want := range map[string]PrivilegeMode{
		"":         PrivilegeAuto,
		"AUTO":     PrivilegeAuto,
		" never ":  PrivilegeNever,
		"required": PrivilegeRequired,
	} {
		got, err := ParsePrivilegeMode(input)
		if err != nil || got != want {
			t.Fatalf("ParsePrivilegeMode(%q)=%q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParsePrivilegeMode("always"); err == nil {
		t.Fatal("se esperaba rechazo del modo de privilegio desconocido")
	}
}

func TestPermissionDeniedDetection(t *testing.T) {
	for _, message := range []string{
		"permission denied",
		"SFTP: SSH_FX_PERMISSION_DENIED",
		"operation not permitted",
		"Access denied while opening file",
	} {
		if !IsPermissionDenied(assertError(message)) {
			t.Fatalf("no se detectó acceso denegado en %q", message)
		}
	}
	if IsPermissionDenied(assertError("no such file or directory")) {
		t.Fatal("un archivo ausente no debe interpretarse como acceso denegado")
	}
}

func TestElevatedCommandKeepsSudoPasswordOutOfCommand(t *testing.T) {
	password := "s3cr'et-secret"
	wrapped, input, pty, err := elevatedCommand(privilegeState{
		Checked: true, Command: "sudo", Password: password, RequirePTY: true,
	}, "cp '/tmp/a' '/opt/app/a'")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wrapped, password) {
		t.Fatalf("la contraseña sudo apareció dentro del comando: %s", wrapped)
	}
	if input != password+"\n" {
		t.Fatalf("stdin sudo=%q", input)
	}
	if !pty {
		t.Fatal("se perdió el requisito de PTY de sudo")
	}
	if !strings.Contains(wrapped, "sudo -S -p '' -- sh -c") {
		t.Fatalf("comando elevado inesperado: %s", wrapped)
	}
}

func TestNoSuchFileDoesNotTreatMissingUtilityAsMissingPath(t *testing.T) {
	if noSuchFileResult(ExecResult{Stderr: "sh: stat: not found"}) {
		t.Fatal("una utilidad ausente no debe confundirse con una ruta inexistente")
	}
	if !noSuchFileResult(ExecResult{Stderr: "stat: cannot stat '/opt/app': No such file or directory"}) {
		t.Fatal("no se detectó una ruta remota inexistente")
	}
}

func TestLocalTransferValidationStopsLocalErrorsBeforeElevation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "release.zip")
	if err := os.WriteFile(source, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := validateLocalUploadSource(source)
	if err != nil || got != source {
		t.Fatalf("validar subida=%q, %v", got, err)
	}
	if _, err = validateLocalUploadSource(dir); err == nil {
		t.Fatal("una carpeta local no debe tratarse como archivo para upload")
	}
	destination := filepath.Join(dir, "download.zip")
	if err = os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = validateLocalDownloadDestination(destination, false); err == nil || !isNonPrivilegeOperationError(err) {
		t.Fatalf("destino existente debe fallar sin sugerir sudo: %v", err)
	}
}

func TestSemanticFileErrorsDoNotTriggerPrivilegeFallback(t *testing.T) {
	for _, message := range []string{
		"el archivo remoto ya existe",
		"file exists",
		"directory not empty",
		"is a directory",
	} {
		if !isNonPrivilegeOperationError(assertError(message)) {
			t.Fatalf("no se clasificó error semántico %q", message)
		}
	}
}

func TestParentWriteProbePathAlwaysTargetsParent(t *testing.T) {
	got := parentWriteProbePath("/opt/app/current")
	if got != "/opt/app/.lilith-permission-probe" {
		t.Fatalf("ruta de prueba=%q", got)
	}
}

func TestInstallStageCommandPreservesDestinationAndCreateOnly(t *testing.T) {
	command := installStageCommand("/tmp/.lilith-upload", "/opt/app/release.zip", 0o640, false)
	for _, want := range []string{
		"exit 73",
		"exit 74",
		"mkdir -p",
		"stat -c '%u:%g'",
		"chmod \"$perms\"",
		"mv -f",
		"rm -f",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("comando de instalación sin %q:\n%s", want, command)
		}
	}
}

func TestPrivilegedDeleteRejectsRemoteRoot(t *testing.T) {
	connection := &Connection{CWD: "/"}
	if _, err := connection.DeleteWithPrivilege(context.Background(), "/", true, PrivilegeNever); err == nil {
		t.Fatal("la eliminación privilegiada de / debe rechazarse antes de tocar la red")
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
