package distribution

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/moduleapi"
)

func TestPublicDistributionLinksFeatureModulesWithoutCompatibilityMegaModule(t *testing.T) {
	reg := moduleapi.NewRegistry(moduleapi.Catalog(), nil)
	wantModules := []string{
		"core.agents", "core.compaction", "core.config", "core.fork", "core.goal",
		"core.help", "core.mcp", "core.memory", "core.mode", "core.modules",
		"core.plugins", "core.project", "core.providers", "core.rewind", "core.session",
		"core.shell", "core.skills",
	}
	statuses := reg.Statuses()
	if len(statuses) != len(wantModules) {
		t.Fatalf("módulos públicos=%d, esperados=%d: %#v", len(statuses), len(wantModules), statuses)
	}
	seen := map[string]bool{}
	for _, st := range statuses {
		if !st.Enabled {
			t.Fatalf("módulo público %s deshabilitado: %s", st.ID, st.Reason)
		}
		if st.ID == "core.commands" {
			t.Fatal("core.commands no debe volver a enlazarse")
		}
		seen[st.ID] = true
	}
	for _, id := range wantModules {
		if !seen[id] {
			t.Fatalf("falta módulo público %s", id)
		}
	}
	if got := len(reg.Commands()); got != 25 {
		t.Fatalf("slash commands públicos=%d, esperados=25", got)
	}
	for _, cmd := range reg.Commands() {
		_, owner, ok := reg.FindCommand(cmd.Name)
		if !ok || strings.TrimSpace(owner) == "" {
			t.Fatalf("/%s no tiene owner modular", cmd.Name)
		}
	}
	_, owner, target, ok := reg.MatchRoute("/skill:frontend-development")
	if !ok || owner != "core.skills" || target != "frontend-development" {
		t.Fatalf("route skill inválida: ok=%v owner=%q target=%q", ok, owner, target)
	}
}
