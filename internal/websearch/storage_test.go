package websearch

import "testing"

func TestProviderRequiresKeyValidationAndEnabledState(t *testing.T) {
	dir := t.TempDir()
	if HasAvailable(dir) {
		t.Fatal("empty configuration must not expose web search")
	}
	if err := SaveAPIKey(dir, Tavily, "tvly-test-secret"); err != nil {
		t.Fatal(err)
	}
	s, a, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := Resolve(Tavily, s, a)
	if !state.Configured || state.Validated || state.Available {
		t.Fatalf("saved key must remain pending validation: %+v", state)
	}
	if err := RecordTest(dir, Tavily, true, "ok"); err != nil {
		t.Fatal(err)
	}
	if !HasAvailable(dir) {
		t.Fatal("successful validation must expose web search")
	}
	if err := SetEnabled(dir, Tavily, false); err != nil {
		t.Fatal(err)
	}
	if HasAvailable(dir) {
		t.Fatal("disabled provider must hide web search")
	}
}

func TestReplacingKeyInvalidatesPreviousValidation(t *testing.T) {
	dir := t.TempDir()
	if err := SaveAPIKey(dir, Brave, "old-secret"); err != nil {
		t.Fatal(err)
	}
	if err := RecordTest(dir, Brave, true, "ok"); err != nil {
		t.Fatal(err)
	}
	if !HasAvailable(dir) {
		t.Fatal("expected validated provider")
	}
	if err := SaveAPIKey(dir, Brave, "new-secret"); err != nil {
		t.Fatal(err)
	}
	if HasAvailable(dir) {
		t.Fatal("new credential must be revalidated")
	}
}
