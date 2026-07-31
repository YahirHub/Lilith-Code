package ansi

import (
	"strings"
	"testing"
)

func TestStripRemovesCSIAndOSC(t *testing.T) {
	input := "\x1b[31mrojo\x1b[0m \x1b]0;titulo\x07fin"
	if got := Strip(input); got != "rojo fin" {
		t.Fatalf("Strip() = %q", got)
	}
}

func TestWrapPreservesANSIAndCellWidth(t *testing.T) {
	input := "\x1b[31mab界cd\x1b[0m"
	got := Wrap(input, 4)
	if !strings.Contains(got, "\x1b[31m") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("Wrap perdió estilos ANSI: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if width := StringWidth(line); width > 4 {
			t.Fatalf("fila de %d celdas excede el límite: %q", width, line)
		}
	}
	if stripped := strings.ReplaceAll(Strip(got), "\n", ""); stripped != "ab界cd" {
		t.Fatalf("Wrap alteró el contenido: %q", stripped)
	}
}

func TestTruncatePreservesANSIAndGraphemes(t *testing.T) {
	input := "\x1b[32ma界bc\x1b[0m"
	got := Truncate(input, 4, "…")
	if !strings.Contains(got, "\x1b[32m") || !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("Truncate perdió o dejó abierto el estilo ANSI: %q", got)
	}
	if width := StringWidth(got); width > 4 {
		t.Fatalf("Truncate produjo %d celdas: %q", width, got)
	}
	if plain := Strip(got); plain != "a界…" {
		t.Fatalf("contenido truncado inesperado: %q", plain)
	}
}
