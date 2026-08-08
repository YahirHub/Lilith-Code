package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/lilith/li/internal/knowledge"
)

func TestKnowledgeToolsAreLazyAvailableAndBounded(t *testing.T) {
	env := Env{Knowledge: knowledge.NewBuiltin()}
	selected := SelectAvailable("¿Cómo encadeno comandos en PowerShell 5.1?", env)
	if containsKnowledgeName(selected, "knowledge_search") || containsKnowledgeName(selected, "knowledge_read") {
		t.Fatalf("knowledge schemas should remain lazy until explicitly discovered: %v", selected)
	}
	var materialized []string
	env.Materialize = func(names []string) { materialized = append(materialized, names...) }
	if _, err := Execute(context.Background(), "tool_search", map[string]any{"query": "local knowledge PowerShell"}, env); err != nil {
		t.Fatal(err)
	}
	if !containsKnowledgeName(materialized, "knowledge_search") || !containsKnowledgeName(materialized, "knowledge_read") || !containsKnowledgeName(materialized, "knowledge_topics") {
		t.Fatalf("knowledge tools not discovered: %v", materialized)
	}
	out, err := Execute(context.Background(), "knowledge_search", map[string]any{"query": "PowerShell 5.1 &&", "limit": 2}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "public/windows/powershell.md") {
		t.Fatalf("unexpected search: %s", out)
	}
}

func containsKnowledgeName(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}
