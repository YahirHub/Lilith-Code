package tui

import (
	"testing"

	"github.com/lilith/li/internal/providers"
)

func TestProvidersCommandOpensInteractiveScreen(t *testing.T) {
	ctx := &AppContext{Styles: NewStyles(DefaultTheme()), Width: 100, Height: 40}
	cmd := FindCommand("providers")
	if cmd == nil {
		t.Fatal("/providers command not found")
	}
	teaCmd := cmd.Run(ctx, nil, "")
	if teaCmd == nil {
		t.Fatal("/providers must switch to an interactive screen")
	}
	msg := teaCmd()
	switchMsg, ok := msg.(switchScreenMsg)
	if !ok {
		t.Fatalf("message type = %T, want switchScreenMsg", msg)
	}
	if _, ok := switchMsg.next.(*ProviderScreen); !ok {
		t.Fatalf("next screen = %T, want *ProviderScreen", switchMsg.next)
	}
}

func TestProviderScreenLayoutExposesCardsAndCustomStreamingSwitch(t *testing.T) {
	ctx := &AppContext{
		Styles: NewStyles(DefaultTheme()),
		Width:  100,
		Height: 40,
		Providers: providers.Config{
			ActiveProviderID: "custom",
			ActiveModelID:    "m1",
			Providers: []providers.Provider{
				{ID: "custom", Name: "Custom", BaseURL: "https://example.com/v1", Models: []providers.Model{{ID: "m1"}}},
				{ID: providers.OpenCodeFreeID, Name: providers.OpenCodeFreeName, Bundled: true, Auth: providers.AuthBundled, Models: []providers.Model{{ID: "free"}}},
			},
		},
	}
	m := NewProviderScreen(ctx)
	_, hits := m.layout()
	seenCard := false
	seenStreaming := false
	for _, hit := range hits {
		switch hit.id {
		case "provider:custom":
			seenCard = true
		case "streaming":
			seenStreaming = true
		}
	}
	if !seenCard || !seenStreaming {
		t.Fatalf("expected provider card and streaming hit, card=%v streaming=%v", seenCard, seenStreaming)
	}

	m.selected = 1
	_, hits = m.layout()
	for _, hit := range hits {
		if hit.id == "streaming" {
			t.Fatal("bundled provider must render streaming control disabled/non-clickable")
		}
	}
}

func TestProviderVisibleRangeAlwaysKeepsSelectionVisible(t *testing.T) {
	cards := []settingsBlock{
		{text: "a\na"},
		{text: "b\nb\nb"},
		{text: "c\nc"},
		{text: "d\nd"},
	}
	start, end := providerVisibleRange(cards, 2, 6)
	if start > 2 || end <= 2 {
		t.Fatalf("selection 2 not visible in range %d:%d", start, end)
	}
	if start < 0 || end > len(cards) || start >= end {
		t.Fatalf("invalid visible range %d:%d", start, end)
	}
}
