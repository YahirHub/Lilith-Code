package providers

import (
	"testing"

	"github.com/lilith/li/internal/secrets"
)

func TestProviderManagementStreamingAndDelete(t *testing.T) {
	dir := t.TempDir()
	p, err := Upsert(dir, UpsertParams{
		Name:        "Custom",
		BaseURL:     "https://example.com/v1",
		APIKeyInput: "secret",
		Models:      []Model{{ID: "model-a", MaxContextTokens: 128000}},
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := SetUseNonStreaming(dir, p.ID, true); err != nil {
		t.Fatalf("SetUseNonStreaming() error = %v", err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.FindProvider(p.ID)
	if got == nil || !got.UseNonStreaming {
		t.Fatalf("provider after toggle = %+v", got)
	}

	if err := Delete(dir, p.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatalf("Load() after delete error = %v", err)
	}
	if cfg.FindProvider(p.ID) != nil {
		t.Fatal("deleted provider is still persisted")
	}
	if cfg.ActiveProviderID != "" || cfg.ActiveModelID != "" {
		t.Fatalf("active selection should be cleared, got %q/%q", cfg.ActiveProviderID, cfg.ActiveModelID)
	}
	auth, err := secrets.Load(dir)
	if err != nil {
		t.Fatalf("secrets.Load() error = %v", err)
	}
	if _, ok := auth.APIKeys[p.ID]; ok {
		t.Fatal("deleted provider API key is still persisted")
	}
}

func TestProviderManagementRejectsBundledOrMissingIDs(t *testing.T) {
	dir := t.TempDir()
	if err := SetUseNonStreaming(dir, OpenCodeFreeID, true); err == nil {
		t.Fatal("bundled provider should not be mutable through persisted provider settings")
	}
	if err := Delete(dir, OpenCodeFreeID); err == nil {
		t.Fatal("bundled provider should not be deletable")
	}
}
