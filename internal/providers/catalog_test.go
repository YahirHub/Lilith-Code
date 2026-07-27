package providers

import "testing"

func TestEnrichAppliesCatalogContext(t *testing.T) {
	out := Enrich([]Model{{ID: "opencode/deepseek-v4_pro"}, {ID: "raro-1", MaxContextTokens: 5000}})
	if out[0].MaxContextTokens != 1_000_000 {
		t.Fatalf("contexto heredado = %d", out[0].MaxContextTokens)
	}
	if out[0].Name == "" {
		t.Fatal("se esperaba un nombre legible del catálogo")
	}
	if out[1].MaxContextTokens != 5000 {
		t.Fatal("no debe sobrescribir un contexto explícito")
	}
}

func TestResolveKeyInput(t *testing.T) {
	if ResolveKeyInput("  none ") != "" || ResolveKeyInput("") != "" {
		t.Fatal("none/vacío deben resolver a credencial vacía")
	}
	if ResolveKeyInput("sk-abc") != "sk-abc" {
		t.Fatal("la clave literal debe conservarse")
	}
	t.Setenv("LI_TEST_KEY", "valor")
	if ResolveKeyInput("env:LI_TEST_KEY") != "valor" {
		t.Fatal("env: debe leer la variable")
	}
}

func TestProviderContextWindowFallsBackToCatalog(t *testing.T) {
	p := &Provider{Models: []Model{{ID: "gpt-5.4"}}}
	if got := p.ContextWindow("gpt-5.4"); got != 400_000 {
		t.Fatalf("ventana = %d", got)
	}
}
