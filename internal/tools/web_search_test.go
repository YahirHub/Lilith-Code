package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/lilith/li/internal/websearch"
)

func TestWebSearchHiddenUntilValidatedProviderExists(t *testing.T) {
	dir := t.TempDir()
	env := Env{Root: dir, ConfigDir: dir}
	active := SelectAvailable("busca en la web la versión actual", env)
	if containsName(active, "web_search") {
		t.Fatalf("web_search leaked without configuration: %v", active)
	}
	var materialized []string
	env.Materialize = func(names []string) { materialized = append(materialized, names...) }
	out, err := Execute(context.Background(), "tool_search", map[string]any{"query": "web search current internet"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "web_search") || containsName(materialized, "web_search") {
		t.Fatalf("tool_search leaked unavailable web_search: %q %v", out, materialized)
	}

	if err := websearch.SaveAPIKey(dir, websearch.Tavily, "test-secret"); err != nil {
		t.Fatal(err)
	}
	if err := websearch.RecordTest(dir, websearch.Tavily, true, "ok"); err != nil {
		t.Fatal(err)
	}
	active = SelectAvailable("busca en la web la versión actual", env)
	if !containsName(active, "web_search") {
		t.Fatalf("validated provider should expose web_search: %v", active)
	}
	materialized = nil
	out, err = Execute(context.Background(), "tool_search", map[string]any{"query": "web search current internet"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "web_search") || !containsName(materialized, "web_search") {
		t.Fatalf("tool_search did not expose validated web_search: %q %v", out, materialized)
	}
}
