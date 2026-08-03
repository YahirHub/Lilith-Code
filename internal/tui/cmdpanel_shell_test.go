package tui

import (
	"strings"
	"testing"
)

func TestCommandPanelTracksRequestedAndResolvedShell(t *testing.T) {
	panel := &CommandPanel{}
	panel.Update(`{"command":"go test ./...","shell":"powershell"}`)
	if panel.Command != "go test ./..." || panel.Shell != "powershell" {
		t.Fatalf("streamed panel=%+v", panel)
	}
	panel.Finish("shell: powershell (C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe)\nexit_code: 0\n")
	if !strings.Contains(panel.ResolvedShell, "powershell") {
		t.Fatalf("resolved shell=%q", panel.ResolvedShell)
	}
	if panel.Failed || panel.ExitCode != 0 {
		t.Fatalf("finished panel=%+v", panel)
	}
}
