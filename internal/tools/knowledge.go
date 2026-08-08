package tools

import (
	"context"
	"encoding/json"
)

func init() {
	available := func(env Env) bool { return env.Knowledge != nil && env.Knowledge.Available() }
	register(Definition{
		Name: "knowledge_search",
		Description: "Search Lilith's local read-only Knowledge Base for exact operational facts and platform syntax. " +
			"Knowledge is separate from Agent Skills and project files. Search before guessing uncertain PowerShell/CMD, Windows, Linux, Termux, Git/GitHub, Docker/Compose or Lilith architecture details. Results return canonical namespace paths for knowledge_read.",
		PromptSnippet: "Search local operational references before guessing exact syntax",
		PromptGuidelines: []string{
			"When platform, tool, version, quoting or command syntax is uncertain, search Knowledge before answering or executing. Do not substitute a plausible guess.",
			"Treat Knowledge as reference material, not as a workflow trigger; Agent Skills remain separate and may point to Knowledge topics.",
		},
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"query":     map[string]any{"type": "string", "description": "Facts or syntax to find."},
			"namespace": map[string]any{"type": "string", "description": "Optional namespace, for example public or company."},
			"topic":     map[string]any{"type": "string", "description": "Optional topic returned by knowledge_topics."},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 24, "default": 8},
		}, "required": []string{"query"}},
		Available: available,
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			matches, err := env.Knowledge.Search(str(args, "query"), str(args, "namespace"), str(args, "topic"), intArg(args, "limit", 8))
			if err != nil {
				return "", err
			}
			return marshalKnowledge(map[string]any{"matches": matches, "count": len(matches)})
		},
	})
	register(Definition{
		Name: "knowledge_read",
		Description: "Read a bounded line range from one local Knowledge document using the canonical namespace/path returned by knowledge_search. " +
			"This does not scan the project or activate a skill.",
		PromptSnippet: "Read a bounded local Knowledge document",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Canonical path such as public/windows/powershell.md."},
			"offset": map[string]any{"type": "integer", "minimum": 1, "default": 1},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 500, "default": 160},
		}, "required": []string{"path"}},
		Available: available,
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			result, err := env.Knowledge.Read(str(args, "path"), intArg(args, "offset", 1), intArg(args, "limit", 160))
			if err != nil {
				return "", err
			}
			return marshalKnowledge(result)
		},
	})
	register(Definition{
		Name: "knowledge_topics",
		Description: "List topics and document counts in the local read-only Knowledge Base, optionally restricted to one namespace. " +
			"Use it when the right reference area is unknown.",
		PromptSnippet: "List local Knowledge namespaces and topics",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Optional namespace, for example public or company."},
		}},
		Available: available,
		Run: func(_ context.Context, args map[string]any, env Env) (string, error) {
			topics, err := env.Knowledge.Topics(str(args, "namespace"))
			if err != nil {
				return "", err
			}
			return marshalKnowledge(map[string]any{"topics": topics, "count": len(topics)})
		},
	})
}

func marshalKnowledge(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
