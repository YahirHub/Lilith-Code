package providers

import "strings"

// Bundled provider identifiers.
const (
	OpenCodeFreeID      = "opencode-free"
	OpenCodeFreeName    = "OpenCode Free"
	OpenCodeFreeBaseURL = "https://opencode.ai/zen/v1"

	// ChatGPTCodexID identifica el proveedor OAuth de la suscripción ChatGPT.
	ChatGPTCodexID      = "openai-codex"
	ChatGPTCodexName    = "ChatGPT Plus/Pro (Codex)"
	ChatGPTCodexBaseURL = "https://chatgpt.com/backend-api/codex"
)

// fallbackFreeModels mirrors los IDs actualmente publicados en
// https://opencode.ai/zen/v1/models con sufijo `-free`. Se usan como fallback
// cuando la API no está accesible.

var fallbackFreeModels = []Model{
	{ID: "deepseek-v4-flash-free", Name: "DeepSeek V4 Flash (Free)", MaxContextTokens: 1_000_000},
	{ID: "mimo-v2.5-free", Name: "MiMo V2.5 (Free)", MaxContextTokens: 128_000},
	{ID: "ling-3.0-flash-free", Name: "Ling 3.0 Flash (Free)", MaxContextTokens: 128_000},
	{ID: "nemotron-3-ultra-free", Name: "Nemotron 3 Ultra (Free)", MaxContextTokens: 1_000_000},
	{ID: "north-mini-code-free", Name: "North Mini Code (Free)", MaxContextTokens: 128_000},
	{ID: "laguna-s-2.1-free", Name: "Laguna S 2.1 (Free)", MaxContextTokens: 128_000},
}

// codexModels es el catálogo Codex expuesto tras un login OAuth.
var codexModels = []Model{
	{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol"},
	{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra"},
	{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna"},
	{ID: "gpt-5.5", Name: "GPT-5.5"},
	{ID: "gpt-5.4", Name: "GPT-5.4"},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 mini"},
	{ID: "gpt-5.3-codex-spark", Name: "GPT-5.3 Codex Spark"},
}

// BundledProviders returns offline fallback catalogs. Live model discovery is
// centralized in RefreshConnectedModels so every provider follows the same
// connection checks, cache and `/models` refresh path. Codex still requires
// completing `/login` before its models become selectable.
func BundledProviders() []Provider {
	return []Provider{
		{
			ID:      OpenCodeFreeID,
			Name:    OpenCodeFreeName,
			BaseURL: OpenCodeFreeBaseURL,
			Auth:    AuthBundled,
			Bundled: true,
			Models:  cloneModels(fallbackFreeModels),
		},
		{
			ID:      ChatGPTCodexID,
			Name:    ChatGPTCodexName,
			BaseURL: ChatGPTCodexBaseURL,
			Auth:    AuthOAuth,
			Bundled: true,
			Models:  cloneModels(codexModels),
		},
	}
}

func cloneModels(in []Model) []Model {
	out := make([]Model, len(in))
	copy(out, in)
	return out
}

func titleFromID(id string) string {
	base := strings.TrimSuffix(strings.ToLower(id), "-free")
	parts := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_' || r == '/'
	})
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ") + " (Free)"
}
