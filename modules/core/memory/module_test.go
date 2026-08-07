package memory

import (
	"errors"
	"testing"

	"github.com/lilith/li/internal/moduleapi"
)

type memoryHost struct {
	enabled *bool
	summary string
	setErr  error
	system  []string
	errors  []string
}

func (h *memoryHost) ConfigDir() string                  { return "" }
func (h *memoryHost) ProjectRoot() string                { return "" }
func (h *memoryHost) AddSystem(v string)                 { h.system = append(h.system, v) }
func (h *memoryHost) AddError(v string)                  { h.errors = append(h.errors, v) }
func (h *memoryHost) ModuleStatuses() []moduleapi.Status { return nil }
func (h *memoryHost) MemorySummary() string              { return h.summary }
func (h *memoryHost) SetAutoMemory(v bool) error {
	h.enabled = new(bool)
	*h.enabled = v
	return h.setErr
}

func TestMemoryCommandOwnsParsingAndMessages(t *testing.T) {
	h := &memoryHost{summary: "Memoria automática: ON"}
	run(h, "status")
	if len(h.system) != 1 || h.system[0] != h.summary {
		t.Fatalf("status=%#v", h.system)
	}
	h.system = nil
	run(h, "on")
	if h.enabled == nil || !*h.enabled || len(h.system) != 1 {
		t.Fatalf("on enabled=%v system=%#v", h.enabled, h.system)
	}
	h.system = nil
	h.setErr = errors.New("boom")
	run(h, "off")
	if len(h.errors) != 1 || h.errors[0] != "No se pudo desactivar memoria: boom" {
		t.Fatalf("errors=%#v", h.errors)
	}
}
