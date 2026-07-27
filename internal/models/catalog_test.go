package models

import "testing"

func TestNormalizeStripsPrefixesAndSuffixes(t *testing.T) {
	cases := map[string]string{
		"opencode/deepseek-v4_pro":         "deepseek-v4-pro",
		"omnirouter/deepseek-v4-flash":     "deepseek-v4-flash",
		"OpenAI/GPT_5.4-latest":            "gpt-5.4",
		"deepseek-v4-flash-free":           "deepseek-v4-flash",
		"anthropic:claude-sonnet-4.5-2026": "claude-sonnet-4.5",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Fatalf("Normalize(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}

func TestMaxContextMatchesAcrossProviders(t *testing.T) {
	if got := MaxContext("omnirouter/deepseek-v4-flash"); got != 1_000_000 {
		t.Fatalf("contexto de deepseek-v4-flash = %d", got)
	}
	if got := MaxContext("opencode/deepseek-v4_pro"); got != 1_000_000 {
		t.Fatalf("contexto de deepseek-v4-pro = %d", got)
	}
	if got := MaxContext("vendor-x/qwen3.7-max"); got != 262_144 {
		t.Fatalf("contexto de qwen3.7-max = %d", got)
	}
	if got := MaxContext("modelo-inventado-xyz"); got != DefaultMaxContext {
		t.Fatalf("modelo desconocido = %d", got)
	}
}

func TestLookupPrefersLongestKey(t *testing.T) {
	spec, ok := Lookup("provider/gpt-5.4-mini-2026-01-01")
	if !ok || spec.Key != "gpt-5.4-mini" {
		t.Fatalf("Lookup devolvió %+v (ok=%v)", spec, ok)
	}
}
