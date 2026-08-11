package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/providers"
)

// ConnectivityState describes why a provider request is waiting before it is
// retried. It deliberately contains no raw socket error text: the TUI can show
// a stable, useful status while the original request remains alive.
type ConnectivityState string

const (
	ConnectivityChecking            ConnectivityState = "checking"
	ConnectivityOffline             ConnectivityState = "offline"
	ConnectivityProviderUnavailable ConnectivityState = "provider_unavailable"
	ConnectivityOnline              ConnectivityState = "online"
)

// RetryStatus is emitted inside Chunk while Stream is recovering from a
// transport interruption. Reset tells the consumer to discard the incomplete
// provider response before the same request is attempted again.
type RetryStatus struct {
	State     ConnectivityState
	Attempt   int
	After     time.Duration
	Reset     bool
	Recovered bool
}

const (
	defaultNetworkRetryMinDelay = time.Second
	defaultNetworkRetryMaxDelay = 15 * time.Second
	defaultConnectivityTimeout  = 4 * time.Second
)

var publicConnectivityURLs = []string{
	"https://connectivitycheck.gstatic.com/generate_204",
	"https://www.cloudflare.com/cdn-cgi/trace",
}

func (c *Client) networkRetryMinDelay() time.Duration {
	if c != nil && c.NetworkRetryMinDelay > 0 {
		return c.NetworkRetryMinDelay
	}
	return defaultNetworkRetryMinDelay
}

func (c *Client) networkRetryMaxDelay() time.Duration {
	if c != nil && c.NetworkRetryMaxDelay > 0 {
		return c.NetworkRetryMaxDelay
	}
	return defaultNetworkRetryMaxDelay
}

func (c *Client) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := c.networkRetryMinDelay()
	maxDelay := c.networkRetryMaxDelay()
	for i := 1; i < attempt && delay < maxDelay; i++ {
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return delay
}

type connectivityProbeResult struct {
	provider  bool
	reachable bool
}

func (c *Client) connectivityState(ctx context.Context, p providers.Provider) ConnectivityState {
	if c != nil && c.ConnectivityProbe != nil {
		return c.ConnectivityProbe(ctx, p)
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	providerURL := strings.TrimSpace(p.BaseURL)
	probeCount := len(publicConnectivityURLs)
	if providerURL != "" {
		probeCount++
	}
	results := make(chan connectivityProbeResult, probeCount)
	var wg sync.WaitGroup
	probe := func(rawURL string, provider bool) {
		defer wg.Done()
		results <- connectivityProbeResult{provider: provider, reachable: c.probeURL(probeCtx, rawURL)}
	}
	if providerURL != "" {
		wg.Add(1)
		go probe(providerURL, true)
	}
	for _, rawURL := range publicConnectivityURLs {
		wg.Add(1)
		go probe(rawURL, false)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	publicReachable := false
	for result := range results {
		if result.provider && result.reachable {
			return ConnectivityOnline
		}
		if !result.provider && result.reachable {
			publicReachable = true
		}
	}
	if publicReachable {
		return ConnectivityProviderUnavailable
	}
	return ConnectivityOffline
}

func (c *Client) probeURL(parent context.Context, rawURL string) bool {
	if rawURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, defaultConnectivityTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false
	}
	client := http.DefaultClient
	if c != nil && c.HTTP != nil {
		client = c.HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	// Any HTTP status proves DNS, routing, TCP and (for HTTPS) TLS are working.
	return true
}

func (c *Client) waitForConnectivity(ctx context.Context, p providers.Provider, attempt int, reset bool, out chan<- Chunk) error {
	// If the endpoint probe succeeds but the real request keeps failing, avoid a
	// hot retry loop. The first interruption is retried immediately; consecutive
	// request-level failures use the same bounded backoff as offline polling.
	initialDelay := time.Duration(0)
	if attempt > 1 {
		initialDelay = c.retryDelay(attempt - 1)
	}
	if !sendChunk(ctx, out, Chunk{Retry: &RetryStatus{
		State:   ConnectivityChecking,
		Attempt: attempt,
		After:   initialDelay,
		Reset:   reset,
	}}) {
		return ctx.Err()
	}
	if err := waitRetryDelay(ctx, initialDelay); err != nil {
		return err
	}

	probeAttempt := 1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := c.connectivityState(ctx, p)
		if state == ConnectivityOnline {
			sendChunk(ctx, out, Chunk{Retry: &RetryStatus{
				State:     ConnectivityOnline,
				Attempt:   attempt,
				Recovered: true,
			}})
			return nil
		}

		delay := c.retryDelay(probeAttempt)
		if !sendChunk(ctx, out, Chunk{Retry: &RetryStatus{
			State:   state,
			Attempt: attempt,
			After:   delay,
		}}) {
			return ctx.Err()
		}
		if err := waitRetryDelay(ctx, delay); err != nil {
			return err
		}
		probeAttempt++
	}
}

func waitRetryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sendChunk(ctx context.Context, out chan<- Chunk, chunk Chunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// isNetworkFailure identifies transport interruptions that can recover without
// changing credentials, model or request payload. HTTP responses are handled
// separately because they prove network reachability.
func isNetworkFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isNetworkFailure(urlErr.Err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	low := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"connection reset", "connection refused", "network is unreachable",
		"network unreachable", "no route to host", "broken pipe", "no such host",
		"temporary failure in name resolution", "server misbehaving",
		"stream sin actividad", "respuesta del modelo sin actividad", "unexpected eof", "connection closed",
		"use of closed network connection", "i/o timeout", "dial tcp", "dial udp",
		"transport connection broken", "client connection lost", "server sent goaway",
		"stream error:", "tls: use of closed connection", "connection timed out",
	} {
		if strings.Contains(low, fragment) {
			return true
		}
	}
	return false
}
