package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseDeny(t *testing.T) {
	r := parseOutput([]byte(`{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"no"}}`))
	if !r.Blocked || r.Reason != "no" {
		t.Fatalf("%+v", r)
	}
}
func TestMatcher(t *testing.T) {
	if !matches("Bash|Edit", "Bash") {
		t.Fatal("expected regex match")
	}
	if matches("Read", "Bash") {
		t.Fatal("unexpected")
	}
}

func TestParseWorktreePath(t *testing.T) {
	r := parseOutput([]byte(`{"hookSpecificOutput":{"hookEventName":"WorktreeCreate","worktreePath":"/tmp/custom-worktree"}}`))
	if r.WorktreePath != "/tmp/custom-worktree" {
		t.Fatalf("unexpected worktree path: %q", r.WorktreePath)
	}
}

func TestParsePluginHooksAndExpandPortablePaths(t *testing.T) {
	r := ParseJSON([]byte(`{"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/format ${CLAUDE_PROJECT_DIR} ${CLAUDE_PLUGIN_DATA}"}]}]}`), "/project")
	if r.Count() != 1 {
		t.Fatalf("hook count=%d", r.Count())
	}
	r.SetPluginContext("/plugin", "/data", "")
	h := r.Entries["PostToolUse"][0].Hooks[0]
	h.ProjectDir = "/worktree"
	got := expandPluginValue(h.Command, h)
	if got != "/plugin/format /worktree /data" {
		t.Fatalf("expanded command=%q", got)
	}
}

func TestParseJSONAcceptsWrappedSettings(t *testing.T) {
	r := ParseJSON([]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"http","url":"https://example.invalid"}]}]}}`), "/project")
	if !r.Has("SessionStart") || r.Count() != 1 {
		t.Fatalf("runner=%#v", r)
	}
}

func TestMCPToolHookResolvesInputAndDoesNotBlockOnFailure(t *testing.T) {
	r := ParseJSON([]byte(`{
		"PostToolUse":[{"matcher":"Write","hooks":[{
			"type":"mcp_tool",
			"server":"plugin:quality:db",
			"tool":"record",
			"input":{"path":"${tool_input.file_path}","metadata":"${tool_input}","label":"saved:${tool_input.file_path}"}
		}]}]
	}`), "/project")
	var gotServer, gotTool string
	var gotInput map[string]any
	r.MCPTool = func(_ context.Context, server, tool string, input map[string]any) (string, error) {
		gotServer, gotTool, gotInput = server, tool, input
		return `{"systemMessage":"recorded"}`, nil
	}
	res, err := r.Run(context.Background(), "PostToolUse", "Write", map[string]any{
		"tool_input": map[string]any{"file_path": "README.md", "mode": "0644"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotServer != "plugin:quality:db" || gotTool != "record" {
		t.Fatalf("call=%s/%s", gotServer, gotTool)
	}
	if gotInput["path"] != "README.md" || gotInput["label"] != "saved:README.md" {
		t.Fatalf("input=%#v", gotInput)
	}
	metadata, ok := gotInput["metadata"].(map[string]any)
	if !ok || metadata["mode"] != "0644" {
		t.Fatalf("metadata=%#v", gotInput["metadata"])
	}
	if res.SystemMessage != "recorded" {
		t.Fatalf("result=%#v", res)
	}

	r.MCPTool = func(context.Context, string, string, map[string]any) (string, error) {
		return "", errors.New("offline")
	}
	res, err = r.Run(context.Background(), "PostToolUse", "Write", map[string]any{"tool_input": map[string]any{}})
	if err != nil || res.Blocked || !strings.Contains(res.SystemMessage, "offline") {
		t.Fatalf("non-blocking failure result=%#v err=%v", res, err)
	}
}
