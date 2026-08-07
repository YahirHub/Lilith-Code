package mode

import (
	"testing"

	"github.com/lilith/li/internal/moduleapi"
)

type modeHost struct {
	mode    string
	plan    string
	system  []string
	errors  []string
	submits []string
	syncs   int
}

func (h *modeHost) ConfigDir() string                  { return "" }
func (h *modeHost) ProjectRoot() string                { return "" }
func (h *modeHost) AddSystem(v string)                 { h.system = append(h.system, v) }
func (h *modeHost) AddError(v string)                  { h.errors = append(h.errors, v) }
func (h *modeHost) ModuleStatuses() []moduleapi.Status { return nil }
func (h *modeHost) AgentMode() string                  { return h.mode }
func (h *modeHost) SetAgentMode(v string)              { h.mode = v }
func (h *modeHost) SyncAgentModeUI()                   { h.syncs++ }
func (h *modeHost) LatestPlan() string                 { return h.plan }
func (h *modeHost) Submit(v string)                    { h.submits = append(h.submits, v) }

func TestPlanCommandPreservesLegacyModeSemantics(t *testing.T) {
	h := &modeHost{mode: "build", plan: "hacer A"}
	runPlan(h, "show")
	if len(h.system) != 1 || h.system[0] != "Plan actual:\n\nhacer A" {
		t.Fatalf("show=%#v", h.system)
	}
	runPlan(h, "")
	if h.mode != "plan" || h.syncs != 1 {
		t.Fatalf("plan default: mode=%q syncs=%d", h.mode, h.syncs)
	}
	runPlan(h, "inspecciona src")
	if h.mode != "plan" || len(h.submits) != 1 || h.submits[0] != "inspecciona src" {
		t.Fatalf("plan submit: mode=%q submits=%#v", h.mode, h.submits)
	}
	if h.syncs != 1 {
		t.Fatalf("la ruta con instrucción no debe añadir un mouse-mode cmd extra: %d", h.syncs)
	}
	runPlan(h, "exit")
	if h.mode != "build" || h.syncs != 2 {
		t.Fatalf("exit: mode=%q syncs=%d", h.mode, h.syncs)
	}
}
