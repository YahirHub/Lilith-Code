// Package imageocr extracts text and approximate spatial structure from local
// images without linking native libraries into the Lilith binary.
package imageocr

import "context"

// Box describes a rectangle in source-image pixels.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Word is one OCR token with its original location.
type Word struct {
	Text       string  `json:"text"`
	Box        Box     `json:"box"`
	Confidence float64 `json:"confidence,omitempty"`
}

// Separator is a probable visual divider detected from image edges.
type Separator struct {
	Orientation string  `json:"orientation"`
	Position    float64 `json:"position"` // normalized 0..1
	Strength    float64 `json:"strength"`
}

// Result is the backend-neutral OCR representation consumed by the tool.
type Result struct {
	Backend    string      `json:"backend"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Text       string      `json:"text"`
	Words      []Word      `json:"words"`
	Separators []Separator `json:"separators,omitempty"`
}

// Options controls local OCR. Languages use Tesseract language codes when the
// external fallback is selected; Windows native OCR uses the user's installed
// profile languages.
type Options struct {
	Path      string
	Languages []string
	ConfigDir string
}

type nativeRecognizer func(context.Context, []byte, []string) (string, int, int, []Word, error)
type tesseractRecognizer func(context.Context, string, []string, string) (string, int, int, []Word, error)
