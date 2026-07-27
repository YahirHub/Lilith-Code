// Package models define el catálogo modular de modelos conocidos y su ventana
// de contexto máxima. El catálogo es independiente del proveedor: si un
// proveedor expone `omnirouter/deepseek-v4-flash` u `opencode/deepseek_v4_pro`,
// la coincidencia por nombre normalizado aplica la misma configuración.
package models

import "strings"

// Spec describe un modelo conocido.
type Spec struct {
	// Key es el identificador canónico normalizado (sin prefijo de proveedor).
	Key string
	// Name es la etiqueta legible.
	Name string
	// MaxContext es la ventana de contexto en tokens.
	MaxContext int
	// MaxOutput es el tope de salida cuando el proveedor lo documenta.
	MaxOutput int
}

// DefaultMaxContext se usa cuando no hay ninguna coincidencia.
const DefaultMaxContext = 128_000

// Catalog es la lista completa de modelos conocidos. Añadir un modelo aquí es
// suficiente para que cualquier proveedor que lo exponga herede su contexto.
var Catalog = []Spec{
	// OpenCode Zen (https://opencode.ai/zen/go/v1/models)
	{Key: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", MaxContext: 1_000_000, MaxOutput: 64_000},
	{Key: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", MaxContext: 1_000_000, MaxOutput: 64_000},
	{Key: "deepseek-v3.2", Name: "DeepSeek V3.2", MaxContext: 163_840},
	{Key: "deepseek-r1", Name: "DeepSeek R1", MaxContext: 163_840},
	{Key: "minimax-m3", Name: "MiniMax M3", MaxContext: 1_000_000},
	{Key: "minimax-m2.7", Name: "MiniMax M2.7", MaxContext: 204_800},
	{Key: "minimax-m2.5", Name: "MiniMax M2.5", MaxContext: 204_800},
	{Key: "minimax-m2", Name: "MiniMax M2", MaxContext: 204_800},
	{Key: "kimi-k3", Name: "Kimi K3", MaxContext: 256_000},
	{Key: "kimi-k2.7-code", Name: "Kimi K2.7 Code", MaxContext: 256_000},
	{Key: "kimi-k2.6", Name: "Kimi K2.6", MaxContext: 256_000},
	{Key: "kimi-k2.5", Name: "Kimi K2.5", MaxContext: 256_000},
	{Key: "kimi-k2", Name: "Kimi K2", MaxContext: 131_072},
	{Key: "glm-5.2", Name: "GLM 5.2", MaxContext: 200_000},
	{Key: "glm-5.1", Name: "GLM 5.1", MaxContext: 200_000},
	{Key: "glm-5", Name: "GLM 5", MaxContext: 200_000},
	{Key: "glm-4.6", Name: "GLM 4.6", MaxContext: 200_000},
	{Key: "qwen3.7-max", Name: "Qwen 3.7 Max", MaxContext: 262_144},
	{Key: "qwen3.7-plus", Name: "Qwen 3.7 Plus", MaxContext: 262_144},
	{Key: "qwen3.6-plus", Name: "Qwen 3.6 Plus", MaxContext: 262_144},
	{Key: "qwen3.5-plus", Name: "Qwen 3.5 Plus", MaxContext: 262_144},
	{Key: "qwen3-max", Name: "Qwen 3 Max", MaxContext: 262_144},
	{Key: "qwen3-coder", Name: "Qwen 3 Coder", MaxContext: 262_144},
	{Key: "mimo-v2.5-pro", Name: "MiMo V2.5 Pro", MaxContext: 256_000},
	{Key: "mimo-v2.5", Name: "MiMo V2.5", MaxContext: 256_000},
	{Key: "mimo-v2-pro", Name: "MiMo V2 Pro", MaxContext: 256_000},
	{Key: "mimo-v2-omni", Name: "MiMo V2 Omni", MaxContext: 256_000},
	{Key: "hy3-preview", Name: "HY3 Preview", MaxContext: 256_000},
	{Key: "hy3", Name: "HY3", MaxContext: 256_000},
	{Key: "grok-4.5", Name: "Grok 4.5", MaxContext: 2_000_000},
	{Key: "grok-4", Name: "Grok 4", MaxContext: 256_000},
	{Key: "grok-code-fast-1", Name: "Grok Code Fast 1", MaxContext: 256_000},
	{Key: "nemotron-3-ultra", Name: "Nemotron 3 Ultra", MaxContext: 1_000_000},
	{Key: "north-mini-code", Name: "North Mini Code", MaxContext: 128_000},
	{Key: "laguna-s-2.1", Name: "Laguna S 2.1", MaxContext: 128_000},
	{Key: "ling-3.0-flash", Name: "Ling 3.0 Flash", MaxContext: 128_000},

	// OpenAI (incluye los modelos disponibles con suscripción Plus/Codex)
	{Key: "gpt-5.6-sol", Name: "GPT-5.6 Sol", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.6-terra", Name: "GPT-5.6 Terra", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.6-luna", Name: "GPT-5.6 Luna", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.6", Name: "GPT-5.6", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.5-codex", Name: "GPT-5.5 Codex", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.5", Name: "GPT-5.5", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.4-codex", Name: "GPT-5.4 Codex", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.4-mini", Name: "GPT-5.4 Mini", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.4-nano", Name: "GPT-5.4 Nano", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.4", Name: "GPT-5.4", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5.2", Name: "GPT-5.2", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5-codex", Name: "GPT-5 Codex", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5-mini", Name: "GPT-5 Mini", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5-nano", Name: "GPT-5 Nano", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-5", Name: "GPT-5", MaxContext: 400_000, MaxOutput: 128_000},
	{Key: "gpt-4.1-mini", Name: "GPT-4.1 Mini", MaxContext: 1_047_576},
	{Key: "gpt-4.1", Name: "GPT-4.1", MaxContext: 1_047_576},
	{Key: "gpt-4o-mini", Name: "GPT-4o Mini", MaxContext: 128_000},
	{Key: "gpt-4o", Name: "GPT-4o", MaxContext: 128_000},
	{Key: "o4-mini", Name: "o4-mini", MaxContext: 200_000},
	{Key: "o3-mini", Name: "o3-mini", MaxContext: 200_000},
	{Key: "o3", Name: "o3", MaxContext: 200_000},

	// Anthropic
	{Key: "claude-opus-4.5", Name: "Claude Opus 4.5", MaxContext: 200_000},
	{Key: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", MaxContext: 1_000_000},
	{Key: "claude-haiku-4.5", Name: "Claude Haiku 4.5", MaxContext: 200_000},
	{Key: "claude-sonnet-4", Name: "Claude Sonnet 4", MaxContext: 1_000_000},
	{Key: "claude-3.7-sonnet", Name: "Claude 3.7 Sonnet", MaxContext: 200_000},
	{Key: "claude-3.5-sonnet", Name: "Claude 3.5 Sonnet", MaxContext: 200_000},

	// Google
	{Key: "gemini-3.6-flash", Name: "Gemini 3.6 Flash", MaxContext: 1_000_000},
	{Key: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", MaxContext: 1_000_000},
	{Key: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", MaxContext: 1_000_000},
	{Key: "gemini-3-flash", Name: "Gemini 3 Flash", MaxContext: 1_000_000},
	{Key: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", MaxContext: 1_048_576},
	{Key: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", MaxContext: 1_048_576},

	// Meta / Mistral / otros abiertos
	{Key: "llama-4-maverick", Name: "Llama 4 Maverick", MaxContext: 1_000_000},
	{Key: "llama-3.3-70b", Name: "Llama 3.3 70B", MaxContext: 131_072},
	{Key: "mistral-large", Name: "Mistral Large", MaxContext: 131_072},
	{Key: "codestral", Name: "Codestral", MaxContext: 256_000},
	{Key: "gpt-oss-120b", Name: "GPT-OSS 120B", MaxContext: 131_072},
	{Key: "gpt-oss-20b", Name: "GPT-OSS 20B", MaxContext: 131_072},
}

// suffixes que no cambian la identidad del modelo.
var droppableSuffixes = []string{
	"-free", "-latest", "-preview", "-beta", "-stable", "-chat", "-instruct",
	"-thinking", "-high", "-medium", "-low", "-online",
}

// Normalize deja un identificador comparable: sin prefijo de proveedor, en
// minúsculas, con `_` y espacios convertidos a `-` y sin fechas ni sufijos de
// variante ("gpt-5.4-2026-01-01" y "openai/GPT_5.4-latest" → "gpt-5.4").
func Normalize(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.NewReplacer("_", "-", " ", "-", "@", "-").Replace(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	s = trimDateSuffix(s)
	for changed := true; changed; {
		changed = false
		for _, suf := range droppableSuffixes {
			if strings.HasSuffix(s, suf) && len(s) > len(suf) {
				s = strings.TrimSuffix(s, suf)
				changed = true
			}
		}
		s = trimDateSuffix(s)
	}
	return s
}

// trimDateSuffix elimina un sufijo de fecha tipo -20260115 o -2026-01-15.
func trimDateSuffix(s string) string {
	parts := strings.Split(s, "-")
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		if isDigits(last) && (len(last) == 8 || len(last) == 6 || len(last) == 4 || len(last) == 2) {
			parts = parts[:len(parts)-1]
			continue
		}
		break
	}
	return strings.Join(parts, "-")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Lookup busca la especificación de un identificador arbitrario. Primero
// intenta coincidencia exacta normalizada y después la clave conocida más
// larga contenida en el identificador, para tolerar prefijos y sufijos.
func Lookup(id string) (Spec, bool) {
	n := Normalize(id)
	if n == "" {
		return Spec{}, false
	}
	for _, spec := range Catalog {
		if spec.Key == n {
			return spec, true
		}
	}
	best := Spec{}
	found := false
	for _, spec := range Catalog {
		if !strings.Contains(n, spec.Key) {
			continue
		}
		if !found || len(spec.Key) > len(best.Key) {
			best, found = spec, true
		}
	}
	return best, found
}

// MaxContext devuelve la ventana de contexto conocida o el valor por defecto.
func MaxContext(id string) int {
	if spec, ok := Lookup(id); ok && spec.MaxContext > 0 {
		return spec.MaxContext
	}
	return DefaultMaxContext
}

// DisplayName devuelve un nombre legible para el identificador.
func DisplayName(id string) string {
	if spec, ok := Lookup(id); ok {
		return spec.Name
	}
	return strings.TrimSpace(id)
}
