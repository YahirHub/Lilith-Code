package tools

import (
	"encoding/json"
	"testing"
)

func TestBoolArgOrPreservesExplicitFalseAndDefault(t *testing.T) {
	args := map[string]any{"disabled": false}
	if got := boolArgOr(args, "disabled", true); got {
		t.Fatal("explicit false must override the true default")
	}
	if got := boolArgOr(args, "missing", true); !got {
		t.Fatal("missing argument must return the provided default")
	}
}

func TestIntArgOrPreservesZeroNegativeAndJSONNumber(t *testing.T) {
	args := map[string]any{
		"zero":     float64(0),
		"negative": -1,
		"json":     json.Number("45"),
	}
	if got := intArgOr(args, "zero", 9); got != 0 {
		t.Fatalf("zero=%d want=0", got)
	}
	if got := intArgOr(args, "negative", 9); got != -1 {
		t.Fatalf("negative=%d want=-1", got)
	}
	if got := intArgOr(args, "json", 9); got != 45 {
		t.Fatalf("json=%d want=45", got)
	}
	if got := intArgOr(args, "missing", 9); got != 9 {
		t.Fatalf("missing=%d want=9", got)
	}
}
