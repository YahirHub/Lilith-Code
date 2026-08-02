package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lilith/li/internal/providers"
)

func TestNewClientUsesStreamingSafeTransport(t *testing.T) {
	t.Parallel()
	client := NewClient(t.TempDir())
	if client.HTTP.Timeout != 0 {
		t.Fatalf("un timeout total corta streams sanos: %s", client.HTTP.Timeout)
	}
	transport, ok := client.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport inesperado: %T", client.HTTP.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 || transport.TLSHandshakeTimeout <= 0 {
		t.Fatalf("faltan límites de conexión: header=%s tls=%s", transport.ResponseHeaderTimeout, transport.TLSHandshakeTimeout)
	}
}

func TestStreamIdleTimeoutUnblocksSilentConnection(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), StreamIdleTimeout: 40 * time.Millisecond}
	out := make(chan Chunk, 1)
	started := time.Now()
	err := client.do(context.Background(), Request{
		Provider: providers.Provider{ID: "silent", BaseURL: server.URL, Auth: providers.AuthNone},
		Model:    "test", Messages: []Message{{Role: "user", Content: "hola"}}, Stream: true,
	}, &countingSink{out: out})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "stream sin actividad") {
		t.Fatalf("se esperaba timeout por inactividad, obtuvo %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("la conexión silenciosa tardó demasiado en desbloquearse: %s", elapsed)
	}
}
