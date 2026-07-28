package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAdaptiveTextAreaGrowsWithoutTruncatingValue(t *testing.T) {
	input := newAdaptiveTextArea("", 1, 4)
	input.SetWidth(40)
	input.SetValue("uno")
	if got := input.Height(); got != 1 {
		t.Fatalf("height one line = %d, want 1", got)
	}
	input.SetValue("uno\ndos\ntres")
	if got := input.Height(); got != 3 {
		t.Fatalf("height three lines = %d, want 3", got)
	}
	value := strings.Join([]string{"1", "2", "3", "4", "5", "6", "7"}, "\n")
	input.SetValue(value)
	if got := input.Height(); got != 4 {
		t.Fatalf("height should stop at max, got %d", got)
	}
	if got := input.Value(); got != value {
		t.Fatalf("auto height must not truncate content:\n got %q\nwant %q", got, value)
	}
}

func TestAdaptiveTextAreaAccountsForWrappedContent(t *testing.T) {
	input := newAdaptiveTextArea("", 1, 5)
	input.SetWidth(12)
	input.SetValue(strings.Repeat("x", 40))
	if got := input.Height(); got <= 1 || got > 5 {
		t.Fatalf("wrapped height = %d, want 2..5", got)
	}
}

func TestSettingsButtonGroupWrapsAndKeepsClickableHits(t *testing.T) {
	s := NewStyles(DefaultTheme())
	block := settingsButtonGroup(s, 20,
		settingsButtonSpec{ID: "one", Label: "Uno"},
		settingsButtonSpec{ID: "two", Label: "Dos"},
		settingsButtonSpec{ID: "three", Label: "Tres"},
	)
	if len(block.hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(block.hits))
	}
	wrapped := false
	for _, hit := range block.hits {
		if hit.rect.y > 0 {
			wrapped = true
			break
		}
	}
	if !wrapped {
		t.Fatal("narrow button group should wrap to another row")
	}
}

func TestSettingsSliderSnapsClickToStep(t *testing.T) {
	spec := settingsSliderSpec{Min: 0, Max: 100, Step: 10, Track: 11}
	if got := settingsSliderValue(spec, 7); got != 70 {
		t.Fatalf("slider value = %d, want 70", got)
	}
	if got := settingsSliderValue(spec, 99); got != 100 {
		t.Fatalf("slider clamp = %d, want 100", got)
	}
}

func TestSettingsContentWidthNeverExceedsNarrowTerminal(t *testing.T) {
	for _, width := range []int{5, 10, 20, 80, 140} {
		got := settingsContentWidth(width)
		if width > 0 && got > width {
			t.Fatalf("settingsContentWidth(%d) = %d", width, got)
		}
		if got > 96 {
			t.Fatalf("settingsContentWidth(%d) exceeds max: %d", width, got)
		}
	}
}

func TestAdaptiveTextAreaInsertStringPreservesNewlineAndGrowth(t *testing.T) {
	input := newAdaptiveTextArea("", 1, 4)
	input.SetWidth(40)
	input.SetValue("modelo-a")
	input.InsertString("\nmodelo-b")
	if got := input.Value(); got != "modelo-a\nmodelo-b" {
		t.Fatalf("inserted value = %q", got)
	}
	if got := input.Height(); got != 2 {
		t.Fatalf("height after newline = %d, want 2", got)
	}
}

func TestSettingsCardWrapsLongMetadataWithinConfiguredWidth(t *testing.T) {
	s := NewStyles(DefaultTheme())
	block := settingsCard(s, settingsCardSpec{
		Title:       "Proveedor",
		Description: "Modelo activo: modelo-con-un-nombre-largo",
		Meta:        "https://example.com/una/ruta/extremadamente/larga/que/debe/ajustarse/al/ancho",
		Width:       40,
	})
	for _, line := range strings.Split(block.text, "\n") {
		if got := lipgloss.Width(line); got > 40 {
			t.Fatalf("rendered card line width = %d, want <= 40: %q", got, line)
		}
	}
}

func TestSettingsStepperExposesIncrementAndDecrementHits(t *testing.T) {
	s := NewStyles(DefaultTheme())
	block := settingsStepper(s, settingsStepperSpec{ID: "workers", Label: "Workers", Value: 4})
	parts := map[string]bool{}
	for _, hit := range block.hits {
		parts[hit.part] = true
	}
	if !parts["dec"] || !parts["inc"] {
		t.Fatalf("stepper hits = %+v, want dec and inc", parts)
	}
}
