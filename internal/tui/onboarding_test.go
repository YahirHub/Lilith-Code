package tui

import "testing"

func TestFirstRunOnboardingOffersAllProviderPaths(t *testing.T) {
	want := []OnboardingOption{OptionCustom, OptionCodex, OptionOpenCodeFree}
	if len(onboardingCards) != len(want) {
		t.Fatalf("onboardingCards tiene %d opciones, se esperaban %d", len(onboardingCards), len(want))
	}
	for i, option := range want {
		if onboardingCards[i].option != option {
			t.Fatalf("opción %d = %v, se esperaba %v", i, onboardingCards[i].option, option)
		}
	}
	if onboardingCards[2].title != "Continuar con OpenCode Free" {
		t.Fatalf("título gratuito = %q", onboardingCards[2].title)
	}
}
