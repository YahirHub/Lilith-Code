package tools

import (
	"strings"
	"testing"
)

func TestExtractImageTextRegistered(t *testing.T) {
	t.Parallel()
	definition, ok := Get("extract_image_text")
	if !ok {
		t.Fatal("extract_image_text no está registrada")
	}
	if definition.Mutating {
		t.Fatal("OCR no debe marcarse como mutante")
	}
	if !strings.Contains(strings.ToLower(definition.Description), "spatial") {
		t.Fatalf("descripción no explica la estructura: %s", definition.Description)
	}
}

func TestSelectActivatesImageOCRForScreenshot(t *testing.T) {
	t.Parallel()
	selected := Select("Analiza la captura pantallas/login.png y conserva la estructura de la UI")
	found := false
	for _, name := range selected {
		if name == "extract_image_text" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("herramientas seleccionadas: %v", selected)
	}
}
