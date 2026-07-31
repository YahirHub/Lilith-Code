package imageocr

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTesseractTSVPreservesBoxesAndReadingOrder(t *testing.T) {
	t.Parallel()
	data := []byte("level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"1\t1\t0\t0\t0\t0\t0\t0\t800\t600\t-1\t\n" +
		"5\t1\t1\t1\t1\t1\t40\t30\t80\t20\t95.5\tArchivo\n" +
		"5\t1\t1\t1\t1\t2\t130\t30\t60\t20\t91.0\tEditar\n" +
		"5\t1\t2\t1\t1\t1\t40\t100\t120\t20\t88.0\tProyecto\n")
	text, width, height, words, err := parseTesseractTSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if width != 800 || height != 600 {
		t.Fatalf("dimensiones=%dx%d", width, height)
	}
	if len(words) != 3 {
		t.Fatalf("palabras=%d", len(words))
	}
	if text != "Archivo Editar\nProyecto" {
		t.Fatalf("orden inesperado: %q", text)
	}
	if words[0].Box.X != 40 || words[0].Confidence != 95.5 {
		t.Fatalf("primer bloque inesperado: %#v", words[0])
	}
}

func TestFormatLayoutIncludesSpatialStructure(t *testing.T) {
	t.Parallel()
	result := Result{
		Backend: "test",
		Width:   1000,
		Height:  600,
		Words: []Word{
			{Text: "Menú", Box: Box{X: 20, Y: 30, Width: 80, Height: 20}},
			{Text: "Guardar", Box: Box{X: 800, Y: 30, Width: 100, Height: 20}},
			{Text: "Editor", Box: Box{X: 300, Y: 260, Width: 100, Height: 30}},
		},
		Text:       "Menú Guardar\nEditor",
		Separators: []Separator{{Orientation: "vertical", Position: 0.22, Strength: 0.9}},
	}
	out, err := Format(result, "ui.png", "layout", 80)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Mapa espacial", "Menú", "Guardar", "Editor", "Línea vertical", "x=2.00%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("salida no contiene %q:\n%s", want, out)
		}
	}
}

func TestDetectSeparatorsFindsLongVerticalDivider(t *testing.T) {
	t.Parallel()
	img := image.NewGray(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.SetGray(x, y, color.Gray{Y: 245})
		}
		img.SetGray(60, y, color.Gray{Y: 20})
		img.SetGray(61, y, color.Gray{Y: 20})
	}
	separators := detectSeparators(img)
	found := false
	for _, separator := range separators {
		if separator.Orientation == "vertical" && separator.Position > 0.27 && separator.Position < 0.34 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no se detectó divisor vertical: %#v", separators)
	}
}

func TestRecognizeFallsBackWithoutLinkingNativeOCR(t *testing.T) {
	oldNative, oldTesseract := nativeOCR, tesseractOCR
	defer func() { nativeOCR, tesseractOCR = oldNative, oldTesseract }()
	nativeOCR = func(context.Context, []byte, []string) (string, int, int, []Word, error) {
		return "", 0, 0, nil, errors.New("native unavailable")
	}
	tesseractOCR = func(context.Context, string, []string, string) (string, int, int, []Word, error) {
		return "Hola", 64, 32, []Word{{Text: "Hola", Box: Box{X: 2, Y: 4, Width: 20, Height: 8}}}, nil
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Recognize(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backend != "tesseract.tsv" || result.Text != "Hola" || len(result.Words) != 1 {
		t.Fatalf("resultado inesperado: %#v", result)
	}
}
