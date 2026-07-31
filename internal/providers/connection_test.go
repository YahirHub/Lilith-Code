package providers

import (
	"testing"

	"github.com/lilith/li/internal/secrets"
)

func TestConnectedProvidersHidesOAuthUntilLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Providers: []Provider{
		{ID: OpenCodeFreeID, Name: OpenCodeFreeName, Auth: AuthBundled, Models: []Model{{ID: "free"}}},
		{ID: ChatGPTCodexID, Name: ChatGPTCodexName, Auth: AuthOAuth, Models: []Model{{ID: "codex"}}},
	}}

	connected, err := ConnectedProviders(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(connected) != 1 || connected[0].ID != OpenCodeFreeID {
		t.Fatalf("proveedores antes de login = %#v", connected)
	}

	store, err := secrets.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.OAuth[ChatGPTCodexID] = secrets.OAuthTokens{AccessToken: "token", RefreshToken: "refresh"}
	if err := secrets.Save(dir, store); err != nil {
		t.Fatal(err)
	}
	connected, err = ConnectedProviders(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(connected) != 2 || connected[1].ID != ChatGPTCodexID {
		t.Fatalf("proveedores después de login = %#v", connected)
	}
}

func TestReconcileActiveMovesAwayFromDisconnectedProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ActiveProviderID: ChatGPTCodexID,
		ActiveModelID:    "codex",
		Providers: []Provider{
			{ID: OpenCodeFreeID, Auth: AuthBundled, Models: []Model{{ID: "free"}}},
			{ID: ChatGPTCodexID, Auth: AuthOAuth, Models: []Model{{ID: "codex"}}},
		},
	}
	if err := ReconcileActive(dir, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProviderID != OpenCodeFreeID || cfg.ActiveModelID != "free" {
		t.Fatalf("selección reconciliada = %s/%s", cfg.ActiveProviderID, cfg.ActiveModelID)
	}
}
