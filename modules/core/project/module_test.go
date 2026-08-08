package project

import (
	"testing"

	"github.com/lilith/li/internal/moduleapi"
)

type initHost struct{ instructions string }

func (h *initHost) ConfigDir() string                  { return "" }
func (h *initHost) ProjectRoot() string                { return "" }
func (h *initHost) AddSystem(string)                   {}
func (h *initHost) AddError(string)                    {}
func (h *initHost) ModuleStatuses() []moduleapi.Status { return nil }
func (h *initHost) InitializeProject()                 { h.instructions = "legacy" }
func (h *initHost) InitializeProjectWithInstructions(value string) {
	h.instructions = value
}

func TestInitForwardsOneShotInstructions(t *testing.T) {
	registry := moduleapi.NewRegistry(moduleapi.Catalog(), nil)
	command, _, ok := registry.FindCommand("init")
	if !ok {
		t.Fatal("/init not registered")
	}
	host := &initHost{}
	command.Handler(host, "actualiza la documentación de modules/")
	if host.instructions != "actualiza la documentación de modules/" {
		t.Fatalf("instructions=%q", host.instructions)
	}
}
