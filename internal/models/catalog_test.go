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
	if got := MaxContext("vendor-x/qwen3.7-max"); got != 1_000_000 {
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

func TestCommandCodeCatalogHasExplicitContextForPublishedModels(t *testing.T) {
	models := map[string]int{
		"claude-sonnet-5":                     1_000_000,
		"claude-sonnet-4-6":                   1_000_000,
		"claude-fable-5":                      1_000_000,
		"claude-opus-5":                       1_000_000,
		"claude-opus-4-8":                     1_000_000,
		"claude-opus-4-7":                     1_000_000,
		"claude-haiku-4-5-20251001":           200_000,
		"gpt-5.6-sol":                         400_000,
		"gpt-5.6-terra":                       400_000,
		"gpt-5.6-luna":                        400_000,
		"gpt-5.5":                             400_000,
		"gpt-5.4":                             400_000,
		"gpt-5.3-codex":                       400_000,
		"gpt-5.4-mini":                        400_000,
		"deepseek/deepseek-v4-pro":            1_000_000,
		"deepseek/deepseek-v4-flash":          1_000_000,
		"moonshotai/Kimi-K3":                  256_000,
		"moonshotai/Kimi-K2.7-Code":           256_000,
		"moonshotai/Kimi-K2.7-Code-Highspeed": 256_000,
		"moonshotai/Kimi-K2.6":                256_000,
		"moonshotai/Kimi-K2.5":                256_000,
		"zai-org/GLM-5.2":                     200_000,
		"zai-org/GLM-5.2-Fast":                200_000,
		"zai-org/GLM-5.1":                     200_000,
		"zai-org/GLM-5":                       200_000,
		"MiniMaxAI/MiniMax-M3":                1_000_000,
		"MiniMaxAI/MiniMax-M2.7":              204_800,
		"MiniMaxAI/MiniMax-M2.5":              204_800,
		"xiaomi/mimo-v2.5-pro":                256_000,
		"xiaomi/mimo-v2.5":                    256_000,
		"Qwen/Qwen3.8-Max":                    1_000_000,
		"Qwen/Qwen3.7-Max":                    1_000_000,
		"Qwen/Qwen3.7-Plus":                   1_000_000,
		"Qwen/Qwen3.7-Flash":                  1_000_000,
		"Qwen/Qwen3.6-Max-Preview":            262_144,
		"Qwen/Qwen3.6-Plus":                   1_000_000,
		"stepfun/Step-3.7-Flash":              256_000,
		"stepfun/Step-3.5-Flash":              256_000,
		"tencent/hy3-paid":                    256_000,
		"google/gemini-3.6-flash":             1_048_576,
		"google/gemini-3.5-flash":             1_048_576,
		"google/gemini-3.5-flash-lite":        1_048_576,
		"google/gemini-3.1-flash-lite":        1_048_576,
		"sakana/fugu-ultra":                   1_000_000,
		"nvidia/nemotron-3-ultra-550b-a55b":   1_000_000,
		"thinkingmachines/inkling":            1_000_000,
		"thinkingmachines/inkling-small":      1_000_000,
		"poolside/laguna-s-2.1-free":          128_000,
		"meta/muse-spark-1.1":                 1_048_576,
		"xai/grok-4.5":                        2_000_000,
	}

	for id, wantContext := range models {
		spec, ok := Lookup(id)
		if !ok {
			t.Errorf("Lookup(%q) no encontró un modelo conocido", id)
			continue
		}
		if wantKey := Normalize(id); spec.Key != wantKey {
			t.Errorf("Lookup(%q) resolvió %q, se esperaba coincidencia explícita %q", id, spec.Key, wantKey)
		}
		if spec.MaxContext != wantContext {
			t.Errorf("contexto de %q = %d, se esperaba %d", id, spec.MaxContext, wantContext)
		}
		if spec.Name == "" {
			t.Errorf("%q no tiene nombre visible", id)
		}
	}
}
