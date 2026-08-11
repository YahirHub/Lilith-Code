package openai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseCodexSSERejectsCleanEOFWithoutTerminalEvent(t *testing.T) {
	t.Parallel()

	out := make(chan Chunk, 8)
	sink := &countingSink{ctx: context.Background(), out: out}
	body := io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"parcial\"}\n\n"))

	err := parseCodexSSE(context.Background(), body, time.Second, sink)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error=%v, esperado io.ErrUnexpectedEOF", err)
	}
}

func TestParseCodexSSEAcceptsResponseCompletedWithoutDoneMarker(t *testing.T) {
	t.Parallel()

	out := make(chan Chunk, 8)
	sink := &countingSink{ctx: context.Background(), out: out}
	body := io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{}}\n\n",
	))

	if err := parseCodexSSE(context.Background(), body, time.Second, sink); err != nil {
		t.Fatalf("parseCodexSSE: %v", err)
	}

	select {
	case chunk := <-out:
		if chunk.Delta != "ok" {
			t.Fatalf("delta=%q, esperado ok", chunk.Delta)
		}
	default:
		t.Fatal("faltó delta de salida")
	}
}
