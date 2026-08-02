package tui

import (
	"path/filepath"
	"testing"

	"github.com/lilith/li/internal/codeintel"
)

func TestCodeIntelForReusesManagerForCleanEquivalentRoot(t *testing.T) {
	root := t.TempDir()
	configDir := t.TempDir()
	manager := codeintel.New(root, configDir)
	model := &ChatModel{
		ctx:       &AppContext{ConfigDir: configDir},
		project:   root,
		codeIntel: manager,
	}

	got := model.codeIntelFor(filepath.Join(root, "."))
	if got != manager {
		t.Fatal("codeIntelFor created a second manager for an equivalent clean path")
	}
}
