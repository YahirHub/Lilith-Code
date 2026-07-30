package mcp

import (
	"context"
	"testing"
)

type fakeClient struct {
	tool  string
	input map[string]any
}

func (f *fakeClient) Initialize(context.Context) error          { return nil }
func (f *fakeClient) ListTools(context.Context) ([]Tool, error) { return nil, nil }
func (f *fakeClient) Call(_ context.Context, tool string, input map[string]any) (string, error) {
	f.tool = tool
	f.input = input
	return "ok", nil
}
func (f *fakeClient) Close() error { return nil }

func TestCallServerToolResolvesPluginReference(t *testing.T) {
	client := &fakeClient{}
	r := NewRuntime()
	r.clients["plugin_quality-tools_db-main"] = client
	r.tools["mcp__plugin_quality-tools_db-main__record"] = Tool{
		Server: "plugin_quality-tools_db-main",
		Name:   "record",
	}
	got, err := r.CallServerTool(context.Background(), "plugin:quality-tools:db-main", "record", map[string]any{"path": "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" || client.tool != "record" || client.input["path"] != "README.md" {
		t.Fatalf("got=%q tool=%q input=%#v", got, client.tool, client.input)
	}
}
