package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

type sequenceRoundTripper struct {
	mu        sync.Mutex
	responses []func(*http.Request) (*http.Response, error)
	calls     int
}

func (s *sequenceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.responses) {
		return nil, io.ErrUnexpectedEOF
	}
	fn := s.responses[s.calls]
	s.calls++
	return fn(req)
}

type unexpectedEOFBody struct {
	reader io.Reader
}

func (b *unexpectedEOFBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *unexpectedEOFBody) Close() error               { return nil }

type terminalErrorReader struct {
	err  error
	done bool
}

func (r *terminalErrorReader) Read([]byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return 0, r.err
}

func sseResponse(body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
}

func TestTransientHTTPRecognizesReasoningContentCarryForward400(t *testing.T) {
	t.Parallel()

	err := errors.New(`HTTP 400: {"error":{"type":"invalid_request_error","message":"Error from provider (Console): Upstream request failed: [invalid_request_error] The ` + "`reasoning_content`" + ` in the thinking mode must be passed back to the API."}}`)
	if !isTransientHTTP(err) {
		t.Fatalf("reasoning_content carry-forward 400 should be transient: %v", err)
	}
	if isTransientHTTP(errors.New(`HTTP 400: {"error":{"message":"invalid model"}}`)) {
		t.Fatal("ordinary HTTP 400 must not be retried")
	}
}

func TestStreamSilentlyRetriesReasoningContentCarryForward400(t *testing.T) {
	t.Parallel()

	transport := &sequenceRoundTripper{responses: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Status:     "400 Bad Request",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"Error from provider (Console): Upstream request failed: [invalid_request_error] The reasoning_content in the thinking mode must be passed back to the API."}}`)),
			}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return sseResponse(io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))), nil
		},
	}}
	client := &Client{HTTP: &http.Client{Transport: transport}}

	chunks := collectChunks(client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", BaseURL: "https://provider.test/v1", Auth: providers.AuthNone},
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hola"}},
		Stream:   true,
	}))

	var sawOK, sawDone bool
	for _, chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("transient reasoning_content 400 leaked as terminal error: %v", chunk.Err)
		}
		if chunk.Retry != nil {
			t.Fatalf("service retry should stay silent in the UI, got retry status: %#v", chunk.Retry)
		}
		if chunk.Delta == "ok" {
			sawOK = true
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if transport.calls != 2 || !sawOK || !sawDone {
		t.Fatalf("unexpected retry result: calls=%d ok=%v done=%v chunks=%#v", transport.calls, sawOK, sawDone, chunks)
	}
}

func TestStreamWaitsForConnectivityAndRetriesWithoutRawTCPError(t *testing.T) {
	t.Parallel()
	transport := &sequenceRoundTripper{responses: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network is unreachable")}
		},
		func(*http.Request) (*http.Response, error) {
			return sseResponse(io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))), nil
		},
	}}
	probeCalls := 0
	client := &Client{
		HTTP:                 &http.Client{Transport: transport},
		NetworkRetryMinDelay: time.Millisecond,
		NetworkRetryMaxDelay: time.Millisecond,
		ConnectivityProbe: func(context.Context, providers.Provider) ConnectivityState {
			probeCalls++
			if probeCalls < 3 {
				return ConnectivityOffline
			}
			return ConnectivityOnline
		},
	}

	chunks := collectChunks(client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", Name: "Test", BaseURL: "https://provider.test/v1", Auth: providers.AuthNone},
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hola"}},
		Stream:   true,
	}))

	var sawOffline, sawRecovered, sawDone bool
	for _, chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("network interruption leaked as terminal error: %v", chunk.Err)
		}
		if chunk.Retry != nil && chunk.Retry.State == ConnectivityOffline {
			sawOffline = true
		}
		if chunk.Retry != nil && chunk.Retry.Recovered {
			sawRecovered = true
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if !sawOffline || !sawRecovered || !sawDone {
		t.Fatalf("missing recovery states: offline=%v recovered=%v done=%v chunks=%#v", sawOffline, sawRecovered, sawDone, chunks)
	}
}

func TestRepeatedImmediateNetworkFailuresUseBoundedBackoff(t *testing.T) {
	t.Parallel()
	fail := func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	}
	transport := &sequenceRoundTripper{responses: []func(*http.Request) (*http.Response, error){
		fail,
		fail,
		func(*http.Request) (*http.Response, error) {
			return sseResponse(io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))), nil
		},
	}}
	client := &Client{
		HTTP:                 &http.Client{Transport: transport},
		NetworkRetryMinDelay: 2 * time.Millisecond,
		NetworkRetryMaxDelay: 2 * time.Millisecond,
		ConnectivityProbe: func(context.Context, providers.Provider) ConnectivityState {
			return ConnectivityOnline
		},
	}

	chunks := collectChunks(client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", BaseURL: "https://provider.test/v1", Auth: providers.AuthNone},
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hola"}},
		Stream:   true,
	}))

	var sawSecondDelay, sawDone bool
	for _, chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected terminal error: %v", chunk.Err)
		}
		if chunk.Retry != nil && chunk.Retry.State == ConnectivityChecking && chunk.Retry.Attempt == 2 && chunk.Retry.After > 0 {
			sawSecondDelay = true
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if !sawSecondDelay || !sawDone {
		t.Fatalf("repeated failures did not back off safely: delayed=%v done=%v chunks=%#v", sawSecondDelay, sawDone, chunks)
	}
}

func TestStreamRequestsResetAfterPartialResponseIsInterrupted(t *testing.T) {
	t.Parallel()
	partial := io.MultiReader(
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"parcial\"}}]}\n\n"),
		&terminalErrorReader{err: io.ErrUnexpectedEOF},
	)
	transport := &sequenceRoundTripper{responses: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return sseResponse(&unexpectedEOFBody{reader: partial}), nil
		},
		func(*http.Request) (*http.Response, error) {
			return sseResponse(io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"completa\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))), nil
		},
	}}
	client := &Client{
		HTTP:                 &http.Client{Transport: transport},
		NetworkRetryMinDelay: time.Millisecond,
		NetworkRetryMaxDelay: time.Millisecond,
		ConnectivityProbe: func(context.Context, providers.Provider) ConnectivityState {
			return ConnectivityOnline
		},
	}

	chunks := collectChunks(client.Stream(context.Background(), Request{
		Provider: providers.Provider{ID: "test", Name: "Test", BaseURL: "https://provider.test/v1", Auth: providers.AuthNone},
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hola"}},
		Stream:   true,
	}))

	var sawPartial, sawReset, sawComplete, sawDone bool
	for _, chunk := range chunks {
		if chunk.Delta == "parcial" {
			sawPartial = true
		}
		if chunk.Retry != nil && chunk.Retry.Reset {
			sawReset = true
		}
		if chunk.Delta == "completa" {
			sawComplete = true
		}
		if chunk.Done {
			sawDone = true
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected terminal error: %v", chunk.Err)
		}
	}
	if !sawPartial || !sawReset || !sawComplete || !sawDone {
		t.Fatalf("unexpected retry sequence: partial=%v reset=%v complete=%v done=%v chunks=%#v", sawPartial, sawReset, sawComplete, sawDone, chunks)
	}
}

func collectChunks(ch <-chan Chunk) []Chunk {
	var chunks []Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	return chunks
}
