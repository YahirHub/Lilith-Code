package moduleapi

import (
	"strings"
	"testing"
)

type fakeHost struct{}

func (fakeHost) ConfigDir() string          { return "" }
func (fakeHost) ProjectRoot() string        { return "" }
func (fakeHost) AddSystem(string)           {}
func (fakeHost) AddError(string)            {}
func (fakeHost) InvokeSkill(string, string) {}
func (fakeHost) Submit(string)              {}
func (fakeHost) OpenScreen(Screen)          {}
func (fakeHost) RewindBusy() bool           { return false }
func (fakeHost) ModuleStatuses() []Status   { return nil }

func TestRegistryResolvesCommandAliasAndRoute(t *testing.T) {
	r := NewRegistry([]Definition{{
		ID: "company.demo", Name: "Demo", API: APIVersion,
		Commands: []Command{{Name: "demo", Aliases: []string{"d"}, Handler: func(Host, string) {}}},
		Routes:   []Route{{Prefix: "demo:", Handler: func(Host, string, string) {}}},
	}}, nil)
	if _, id, ok := r.FindCommand("/d"); !ok || id != "company.demo" {
		t.Fatalf("alias no resuelto: ok=%v id=%q", ok, id)
	}
	if _, id, target, ok := r.MatchRoute("/demo:status"); !ok || id != "company.demo" || target != "status" {
		t.Fatalf("route no resuelto: ok=%v id=%q target=%q", ok, id, target)
	}
}

func TestRegistryDisablesMissingDependency(t *testing.T) {
	r := NewRegistry([]Definition{{ID: "company.deploy", API: APIVersion, Requires: []string{"core.git"}, Commands: []Command{{Name: "deploy", Handler: func(Host, string) {}}}}}, nil)
	st := r.Statuses()
	if len(st) != 1 || st[0].Enabled || !strings.Contains(st[0].Reason, "missing required module core.git") {
		t.Fatalf("status inesperado: %+v", st)
	}
	if _, _, ok := r.FindCommand("deploy"); ok {
		t.Fatal("módulo deshabilitado no debe publicar comandos")
	}
}

func TestRegistryFailsClosedOnCoreCommandConflict(t *testing.T) {
	r := NewRegistry([]Definition{{ID: "company.bad", API: APIVersion, Commands: []Command{{Name: "config", Handler: func(Host, string) {}}}}}, []string{"config"})
	st := r.Statuses()
	if len(st) != 1 || st[0].Enabled || !strings.Contains(st[0].Reason, "conflicts") {
		t.Fatalf("colisión no bloqueada: %+v", st)
	}
}

func TestRegistryRejectsIncompatibleAPI(t *testing.T) {
	r := NewRegistry([]Definition{{ID: "company.old", API: APIVersion + 1, Commands: []Command{{Name: "old", Handler: func(Host, string) {}}}}}, nil)
	st := r.Statuses()
	if len(st) != 1 || st[0].Enabled || !strings.Contains(st[0].Reason, "incompatible") {
		t.Fatalf("API incompatible no rechazada: %+v", st)
	}
}

func TestBuiltinModuleWinsConflictRegardlessOfRegistrationOrder(t *testing.T) {
	private := Definition{ID: "company.rewind", Source: "company", API: APIVersion, Commands: []Command{{Name: "rewind", Handler: func(Host, string) {}}}}
	core := Definition{ID: "core.rewind", Source: "builtin", API: APIVersion, Commands: []Command{{Name: "rewind", Handler: func(Host, string) {}}}}
	r := NewRegistry([]Definition{private, core}, nil)
	_, owner, ok := r.FindCommand("rewind")
	if !ok || owner != "core.rewind" {
		t.Fatalf("core debe conservar /rewind: owner=%q ok=%v", owner, ok)
	}
	for _, st := range r.Statuses() {
		if st.ID == "company.rewind" && st.Enabled {
			t.Fatalf("módulo privado no debe sobrescribir core: %+v", st)
		}
	}
}

func TestRegistryOrdersCommandsWithoutLettingPrivateDefaultsDisplaceCore(t *testing.T) {
	r := NewRegistry([]Definition{
		{ID: "company.demo", Source: "company", API: APIVersion, Commands: []Command{{Name: "company", Handler: func(Host, string) {}}}},
		{ID: "core.rewind", Source: "builtin", API: APIVersion, Commands: []Command{{Name: "rewind", Order: 650, Handler: func(Host, string) {}}}},
		{ID: "core.modules", Source: "builtin", API: APIVersion, Commands: []Command{{Name: "modules", Order: 150, Handler: func(Host, string) {}}}},
	}, nil)
	got := r.Commands()
	if len(got) != 3 || got[0].Name != "modules" || got[1].Name != "rewind" || got[2].Name != "company" {
		t.Fatalf("orden inesperado: %+v", got)
	}
}

func TestRegistryRejectsDynamicRouteWithoutColonBoundary(t *testing.T) {
	r := NewRegistry([]Definition{{
		ID: "company.bad-route", Source: "company", API: APIVersion,
		Routes: []Route{{Prefix: "rewind", Handler: func(Host, string, string) {}}},
	}}, nil)
	st := r.Statuses()
	if len(st) != 1 || st[0].Enabled || !strings.Contains(st[0].Reason, "must end with ':'") {
		t.Fatalf("route insegura no rechazada: %+v", st)
	}
}

func TestRegistryRejectsExactCommandInsideExistingDynamicRoute(t *testing.T) {
	r := NewRegistry([]Definition{
		{ID: "core.skills", Source: "builtin", API: APIVersion, Routes: []Route{{Prefix: "skill:", Handler: func(Host, string, string) {}}}},
		{ID: "company.shadow", Source: "company", API: APIVersion, Commands: []Command{{Name: "skill:frontend", Handler: func(Host, string) {}}}},
	}, nil)
	if _, _, ok := r.FindCommand("skill:frontend"); ok {
		t.Fatal("un comando exacto privado no debe interceptar el namespace dinámico de core.skills")
	}
	for _, st := range r.Statuses() {
		if st.ID == "company.shadow" && (st.Enabled || !strings.Contains(st.Reason, "falls inside route")) {
			t.Fatalf("colisión command↔route no bloqueada: %+v", st)
		}
	}
}

func TestRegistryRejectsDynamicRouteContainingExistingExactCommand(t *testing.T) {
	r := NewRegistry([]Definition{
		{ID: "core.demo", Source: "builtin", API: APIVersion, Commands: []Command{{Name: "company:status", Handler: func(Host, string) {}}}},
		{ID: "company.route", Source: "company", API: APIVersion, Routes: []Route{{Prefix: "company:", Handler: func(Host, string, string) {}}}},
	}, nil)
	for _, st := range r.Statuses() {
		if st.ID == "company.route" && (st.Enabled || !strings.Contains(st.Reason, "contains command")) {
			t.Fatalf("colisión route↔command no bloqueada: %+v", st)
		}
	}
}

func TestRegistryReservesCoreNamespaceForBuiltins(t *testing.T) {
	r := NewRegistry([]Definition{{
		ID: "core.private-shadow", Source: "company", API: APIVersion,
		Commands: []Command{{Name: "private-shadow", Handler: func(Host, string) {}}},
	}}, nil)
	st := r.Statuses()
	if len(st) != 1 || st[0].Enabled || !strings.Contains(st[0].Reason, "reserved core.*") {
		t.Fatalf("namespace core.* no fue protegido: %+v", st)
	}
}
