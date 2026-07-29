package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTimeout      = 20 * time.Second
	maxRateLimitRetry   = 2500 * time.Millisecond
	defaultResultCount  = 8
	defaultContextChars = 15000
	providerUserAgent   = "lilith-web-search/1.0"
)

type Depth string

const (
	DepthStandard Depth = "standard"
	DepthDeep     Depth = "deep"
)

type Request struct {
	Query      string
	NumResults int
	Depth      Depth
}

type ResultItem struct {
	Title         string
	URL           string
	Snippet       string
	PublishedDate string
	Author        string
}

type Attempt struct {
	Provider ProviderID
	Success  bool
	Message  string
}

type Result struct {
	Provider ProviderID
	Endpoint string
	Items    []ResultItem
	Attempts []Attempt
	Text     string
}

type HTTPError struct {
	Provider   ProviderID
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return e.Message }

type providerGate struct {
	mu          sync.Mutex
	nextAllowed time.Time
}

var providerGates = map[ProviderID]*providerGate{
	Tavily: {}, Brave: {}, Exa: {}, Linkup: {}, Firecrawl: {}, SerpAPI: {}, Zenserp: {},
}

func providerInterval(id ProviderID) time.Duration {
	if id == Brave {
		return 1100 * time.Millisecond
	}
	return 0
}

func normalizeRequest(in Request) (Request, error) {
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return Request{}, errors.New("la consulta de búsqueda no puede estar vacía")
	}
	if in.NumResults <= 0 {
		in.NumResults = defaultResultCount
	}
	if in.NumResults > 50 {
		in.NumResults = 50
	}
	if in.Depth != DepthDeep {
		in.Depth = DepthStandard
	}
	return in, nil
}

func Run(ctx context.Context, configDir string, in Request) (Result, error) {
	req, err := normalizeRequest(in)
	if err != nil {
		return Result{}, err
	}
	settings, auth, err := Load(configDir)
	if err != nil {
		return Result{}, err
	}
	order := AvailableOrder(settings, auth)
	if len(order) == 0 {
		return Result{}, errors.New("web_search no está disponible: no hay motores habilitados con una API key validada")
	}
	attempts := make([]Attempt, 0, len(order))
	for _, id := range order {
		state := Resolve(id, settings, auth)
		if !state.Available {
			continue
		}
		endpoint, items, runErr := runProvider(ctx, id, req, state.APIKey)
		if runErr == nil && len(items) > 0 {
			items = dedupe(items, req.NumResults)
			if len(items) > 0 {
				attempts = append(attempts, Attempt{Provider: id, Success: true, Message: fmt.Sprintf("%d resultado(s)", len(items))})
				return Result{
					Provider: id,
					Endpoint: endpoint,
					Items:    items,
					Attempts: attempts,
					Text:     formatResults(id, req.Query, items),
				}, nil
			}
			runErr = fmt.Errorf("%s no devolvió resultados utilizables", Labels[id])
		}
		message := "sin resultados"
		if runErr != nil {
			message = redactSecret(runErr.Error(), state.APIKey)
		}
		attempts = append(attempts, Attempt{Provider: id, Message: message})
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
	}
	parts := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		parts = append(parts, Labels[attempt.Provider]+": "+attempt.Message)
	}
	return Result{}, fmt.Errorf("ningún motor configurado pudo completar la búsqueda. %s", strings.Join(parts, " | "))
}

func TestProvider(ctx context.Context, configDir string, id ProviderID) (bool, string) {
	if !ValidProvider(id) {
		return false, "Motor inválido."
	}
	_, auth, err := Load(configDir)
	if err != nil {
		return false, err.Error()
	}
	key := strings.TrimSpace(auth.APIKeys[id])
	if key == "" {
		return false, "No hay API key configurada."
	}
	req := Request{Query: "documentación oficial Go", NumResults: 1, Depth: DepthStandard}
	endpoint, items, err := runProvider(ctx, id, req, key)
	_ = endpoint
	if err != nil {
		return false, redactSecret(err.Error(), key)
	}
	items = dedupe(items, 1)
	if len(items) == 0 {
		return false, "La API respondió, pero no devolvió resultados utilizables."
	}
	return true, "Conexión correcta."
}

func runProvider(ctx context.Context, id ProviderID, req Request, key string) (string, []ResultItem, error) {
	switch id {
	case Tavily:
		return searchTavily(ctx, req, key)
	case Brave:
		return searchBrave(ctx, req, key)
	case Exa:
		return searchExa(ctx, req, key)
	case Linkup:
		return searchLinkup(ctx, req, key)
	case Firecrawl:
		return searchFirecrawl(ctx, req, key)
	case SerpAPI:
		return searchSerpAPI(ctx, req, key)
	case Zenserp:
		return searchZenserp(ctx, req, key)
	default:
		return "", nil, fmt.Errorf("motor no soportado: %s", id)
	}
}

func doJSON(ctx context.Context, id ProviderID, method, endpoint string, headers map[string]string, body any, out any) error {
	gate := providerGates[id]
	if gate == nil {
		gate = &providerGate{}
		providerGates[id] = gate
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()

	for attempt := 0; attempt < 2; attempt++ {
		if wait := time.Until(gate.nextAllowed); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		gate.nextAllowed = time.Now().Add(providerInterval(id))

		var reader io.Reader
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				return err
			}
			reader = bytes.NewReader(encoded)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, method, endpoint, reader)
		if err != nil {
			cancel()
			return err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%s excedió el límite de %ds", Labels[id], int(DefaultTimeout.Seconds()))
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		cancel()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			retry := parseRetryAfter(resp.Header.Get("Retry-After"))
			httpErr := &HTTPError{Provider: id, Status: resp.StatusCode, Message: formatHTTPError(id, resp.StatusCode, data), RetryAfter: retry}
			if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
				delay := retry
				if delay < providerInterval(id) {
					delay = providerInterval(id)
				}
				if delay < time.Second {
					delay = time.Second
				}
				if delay <= maxRateLimitRetry {
					gate.nextAllowed = time.Now().Add(delay)
					continue
				}
			}
			return httpErr
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s devolvió JSON inválido", Labels[id])
		}
		return nil
	}
	return fmt.Errorf("%s no pudo completar la solicitud", Labels[id])
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func formatHTTPError(id ProviderID, status int, data []byte) string {
	if status == http.StatusTooManyRequests {
		return fmt.Sprintf("%s alcanzó temporalmente su límite de solicitudes (HTTP 429)", Labels[id])
	}
	detail := extractError(data)
	if detail == "" {
		return fmt.Sprintf("%s respondió HTTP %d", Labels[id], status)
	}
	return fmt.Sprintf("%s respondió HTTP %d: %s", Labels[id], status, detail)
}

func extractError(data []byte) string {
	var payload map[string]any
	if json.Unmarshal(data, &payload) == nil {
		for _, key := range []string{"detail", "message", "error", "description"} {
			if v := payload[key]; v != nil {
				switch x := v.(type) {
				case string:
					if text := cleanText(x); text != "" {
						return limit(text, 280)
					}
				case map[string]any:
					for _, nested := range []string{"detail", "message", "error", "description"} {
						if text := cleanText(x[nested]); text != "" {
							return limit(text, 280)
						}
					}
				}
			}
		}
	}
	return limit(strings.Join(strings.Fields(string(data)), " "), 280)
}

func commonHeaders() map[string]string {
	return map[string]string{"Accept": "application/json", "User-Agent": providerUserAgent}
}

func searchTavily(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	endpoint := "https://api.tavily.com/search"
	var payload struct {
		Results []struct {
			Title         any `json:"title"`
			URL           any `json:"url"`
			Content       any `json:"content"`
			PublishedDate any `json:"published_date"`
		} `json:"results"`
	}
	headers := commonHeaders()
	headers["Content-Type"] = "application/json"
	headers["Authorization"] = "Bearer " + key
	body := map[string]any{
		"query": req.Query, "search_depth": map[bool]string{true: "advanced", false: "basic"}[req.Depth == DepthDeep],
		"max_results": minInt(req.NumResults, 20), "topic": "general", "include_answer": false,
		"include_raw_content": false, "include_images": false, "safe_search": true,
	}
	if err := doJSON(ctx, Tavily, http.MethodPost, endpoint, headers, body, &payload); err != nil {
		return endpoint, nil, err
	}
	items := make([]ResultItem, 0, len(payload.Results))
	for _, r := range payload.Results {
		if u := safeURL(r.URL); u != "" {
			items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), u), URL: u, Snippet: limit(cleanText(r.Content), 1800), PublishedDate: cleanText(r.PublishedDate)})
		}
	}
	return endpoint, items, nil
}

func searchExa(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	endpoint := "https://api.exa.ai/search"
	var payload struct {
		Results []struct {
			Title         any `json:"title"`
			URL           any `json:"url"`
			PublishedDate any `json:"publishedDate"`
			Author        any `json:"author"`
			Text          any `json:"text"`
			Summary       any `json:"summary"`
			Highlights    any `json:"highlights"`
		} `json:"results"`
	}
	headers := commonHeaders()
	headers["Content-Type"] = "application/json"
	headers["x-api-key"] = key
	typeValue := "auto"
	if req.Depth == DepthDeep {
		typeValue = "deep"
	} else {
		typeValue = "fast"
	}
	body := map[string]any{"query": req.Query, "numResults": req.NumResults, "type": typeValue, "moderation": true, "contents": map[string]any{"highlights": true}}
	if err := doJSON(ctx, Exa, http.MethodPost, endpoint, headers, body, &payload); err != nil {
		return endpoint, nil, err
	}
	maxSnippet := defaultContextChars / maxInt(req.NumResults, 1)
	if maxSnippet < 500 {
		maxSnippet = 500
	}
	if maxSnippet > 4000 {
		maxSnippet = 4000
	}
	items := make([]ResultItem, 0, len(payload.Results))
	for _, r := range payload.Results {
		u := safeURL(r.URL)
		if u == "" {
			continue
		}
		highlights := cleanStringArray(r.Highlights)
		snippet := firstNonEmpty(cleanText(r.Summary), strings.Join(highlights, " "), cleanText(r.Text))
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), u), URL: u, Snippet: limit(snippet, maxSnippet), PublishedDate: cleanText(r.PublishedDate), Author: cleanText(r.Author)})
	}
	return endpoint, items, nil
}

func searchLinkup(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	endpoint := "https://api.linkup.so/v1/search"
	var payload struct {
		Results []struct {
			Name    any `json:"name"`
			Title   any `json:"title"`
			URL     any `json:"url"`
			Content any `json:"content"`
			Date    any `json:"date"`
			Type    any `json:"type"`
		} `json:"results"`
	}
	headers := commonHeaders()
	headers["Content-Type"] = "application/json"
	headers["Authorization"] = "Bearer " + key
	depth := "standard"
	if req.Depth == DepthDeep {
		depth = "deep"
	} else if req.Depth == DepthStandard {
		depth = "fast"
	}
	body := map[string]any{"q": req.Query, "depth": depth, "outputType": "searchResults", "includeImages": false, "maxResults": req.NumResults}
	if err := doJSON(ctx, Linkup, http.MethodPost, endpoint, headers, body, &payload); err != nil {
		return endpoint, nil, err
	}
	items := make([]ResultItem, 0, len(payload.Results))
	for _, r := range payload.Results {
		if strings.EqualFold(cleanText(r.Type), "image") {
			continue
		}
		u := safeURL(r.URL)
		if u == "" {
			continue
		}
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Name), cleanText(r.Title), u), URL: u, Snippet: limit(cleanText(r.Content), 2500), PublishedDate: cleanText(r.Date)})
	}
	return endpoint, items, nil
}

func searchFirecrawl(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	endpoint := "https://api.firecrawl.dev/v2/search"
	var payload struct {
		Success any `json:"success"`
		Error   any `json:"error"`
		Data    struct {
			Web []struct {
				Title       any `json:"title"`
				Description any `json:"description"`
				URL         any `json:"url"`
				Markdown    any `json:"markdown"`
				Metadata    struct {
					Title         any `json:"title"`
					Description   any `json:"description"`
					SourceURL     any `json:"sourceURL"`
					URL           any `json:"url"`
					PublishedTime any `json:"publishedTime"`
					Author        any `json:"author"`
				} `json:"metadata"`
			} `json:"web"`
		} `json:"data"`
	}
	headers := commonHeaders()
	headers["Content-Type"] = "application/json"
	headers["Authorization"] = "Bearer " + key
	body := map[string]any{"query": req.Query, "limit": minInt(req.NumResults, 100), "sources": []string{"web"}, "timeout": int(DefaultTimeout / time.Millisecond), "ignoreInvalidURLs": true}
	if err := doJSON(ctx, Firecrawl, http.MethodPost, endpoint, headers, body, &payload); err != nil {
		return endpoint, nil, err
	}
	if ok, isBool := payload.Success.(bool); isBool && !ok {
		return endpoint, nil, fmt.Errorf("Firecrawl devolvió una respuesta de error: %s", cleanText(payload.Error))
	}
	items := make([]ResultItem, 0, len(payload.Data.Web))
	for _, r := range payload.Data.Web {
		u := firstNonEmpty(safeURL(r.URL), safeURL(r.Metadata.SourceURL), safeURL(r.Metadata.URL))
		if u == "" {
			continue
		}
		items = append(items, ResultItem{
			Title: firstNonEmpty(cleanText(r.Title), cleanText(r.Metadata.Title), u), URL: u,
			Snippet:       limit(firstNonEmpty(cleanText(r.Description), cleanText(r.Metadata.Description), cleanText(r.Markdown)), 2500),
			PublishedDate: cleanText(r.Metadata.PublishedTime), Author: cleanText(r.Metadata.Author),
		})
	}
	return endpoint, items, nil
}

func searchSerpAPI(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	base := "https://serpapi.com/search.json"
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("engine", "google")
	q.Set("q", req.Query)
	q.Set("api_key", key)
	q.Set("num", strconv.Itoa(minInt(req.NumResults, 100)))
	q.Set("safe", "active")
	u.RawQuery = q.Encode()
	var payload struct {
		Error   any `json:"error"`
		Organic []struct {
			Title   any `json:"title"`
			Link    any `json:"link"`
			Snippet any `json:"snippet"`
			Date    any `json:"date"`
			Source  any `json:"source"`
		} `json:"organic_results"`
	}
	if err := doJSON(ctx, SerpAPI, http.MethodGet, u.String(), commonHeaders(), nil, &payload); err != nil {
		return base, nil, err
	}
	if message := cleanText(payload.Error); message != "" {
		return base, nil, fmt.Errorf("SerpApi respondió con error: %s", message)
	}
	items := make([]ResultItem, 0, len(payload.Organic))
	for _, r := range payload.Organic {
		u := safeURL(r.Link)
		if u == "" {
			continue
		}
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), u), URL: u, Snippet: limit(cleanText(r.Snippet), 1800), PublishedDate: cleanText(r.Date), Author: cleanText(r.Source)})
	}
	return base, items, nil
}

func searchZenserp(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	base := "https://app.zenserp.com/api/v2/search"
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("q", req.Query)
	q.Set("engine", "google")
	q.Set("num", strconv.Itoa(minInt(req.NumResults, 100)))
	u.RawQuery = q.Encode()
	headers := commonHeaders()
	headers["apikey"] = key
	var payload struct {
		Error   any `json:"error"`
		Message any `json:"message"`
		Organic []struct {
			Title       any `json:"title"`
			URL         any `json:"url"`
			Link        any `json:"link"`
			Destination any `json:"destination"`
			Description any `json:"description"`
			Date        any `json:"date"`
			Source      any `json:"source"`
		} `json:"organic"`
	}
	if err := doJSON(ctx, Zenserp, http.MethodGet, u.String(), headers, nil, &payload); err != nil {
		return base, nil, err
	}
	if message := firstNonEmpty(cleanText(payload.Error), cleanText(payload.Message)); message != "" {
		return base, nil, fmt.Errorf("Zenserp respondió con error: %s", message)
	}
	items := make([]ResultItem, 0, len(payload.Organic))
	for _, r := range payload.Organic {
		u := firstNonEmpty(safeURL(r.URL), safeURL(r.Link), safeURL(r.Destination))
		if u == "" {
			continue
		}
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), u), URL: u, Snippet: limit(cleanText(r.Description), 1800), PublishedDate: cleanText(r.Date), Author: cleanText(r.Source)})
	}
	return base, items, nil
}

type braveWebResponse struct {
	Web struct {
		Results []struct {
			Title         any `json:"title"`
			URL           any `json:"url"`
			Description   any `json:"description"`
			Age           any `json:"age"`
			PageAge       any `json:"page_age"`
			ExtraSnippets any `json:"extra_snippets"`
		} `json:"results"`
	} `json:"web"`
}

type braveContextItem struct {
	Name     any `json:"name"`
	Title    any `json:"title"`
	URL      any `json:"url"`
	Snippets any `json:"snippets"`
}

type braveContextResponse struct {
	Grounding struct {
		Generic []braveContextItem `json:"generic"`
		Map     []braveContextItem `json:"map"`
		POI     *braveContextItem  `json:"poi"`
	} `json:"grounding"`
	Sources map[string]struct {
		Title any `json:"title"`
		Age   any `json:"age"`
	} `json:"sources"`
}

func searchBrave(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	endpoint, items, err := searchBraveWeb(ctx, req, key)
	if err == nil && len(items) > 0 {
		return endpoint, items, nil
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403 || httpErr.Status == 422 || httpErr.Status == 429) {
		return endpoint, nil, err
	}
	webFailure := "Web Search no devolvió resultados utilizables"
	if err != nil {
		webFailure = err.Error()
	}
	ctxEndpoint, ctxItems, ctxErr := searchBraveContext(ctx, req, key)
	if ctxErr == nil && len(ctxItems) > 0 {
		return ctxEndpoint, ctxItems, nil
	}
	contextFailure := "LLM Context no devolvió resultados utilizables"
	if ctxErr != nil {
		contextFailure = ctxErr.Error()
	}
	return endpoint, nil, fmt.Errorf("Brave falló en Web Search y LLM Context. Web Search: %s. LLM Context: %s", webFailure, contextFailure)
}

func searchBraveWeb(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	base := "https://api.search.brave.com/res/v1/web/search"
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("q", req.Query)
	q.Set("count", strconv.Itoa(minInt(req.NumResults, 20)))
	q.Set("safesearch", "moderate")
	q.Set("text_decorations", "false")
	q.Set("extra_snippets", "true")
	q.Set("result_filter", "web")
	u.RawQuery = q.Encode()
	headers := commonHeaders()
	headers["x-subscription-token"] = key
	var payload braveWebResponse
	if err := doJSON(ctx, Brave, http.MethodGet, u.String(), headers, nil, &payload); err != nil {
		return base, nil, err
	}
	items := make([]ResultItem, 0, len(payload.Web.Results))
	for _, r := range payload.Web.Results {
		u := safeURL(r.URL)
		if u == "" {
			continue
		}
		extra := strings.Join(cleanStringArray(r.ExtraSnippets), " ")
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), u), URL: u, Snippet: limit(strings.TrimSpace(strings.Join(nonEmpty(cleanText(r.Description), extra), " ")), 1800), PublishedDate: firstNonEmpty(cleanText(r.PageAge), cleanText(r.Age))})
	}
	return base, items, nil
}

func bravePublishedDate(v any) string {
	if values := cleanStringArray(v); len(values) > 0 {
		for _, value := range values {
			if len(value) >= 10 && value[4] == '-' && value[7] == '-' {
				return value
			}
		}
		return values[0]
	}
	return cleanText(v)
}

func searchBraveContext(ctx context.Context, req Request, key string) (string, []ResultItem, error) {
	base := "https://api.search.brave.com/res/v1/llm/context"
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("q", req.Query)
	q.Set("count", strconv.Itoa(minInt(maxInt(req.NumResults*2, 8), 50)))
	q.Set("maximum_number_of_urls", strconv.Itoa(minInt(req.NumResults, 50)))
	tokens := defaultContextChars / 4
	if tokens < 1024 {
		tokens = 1024
	}
	if tokens > 32768 {
		tokens = 32768
	}
	q.Set("maximum_number_of_tokens", strconv.Itoa(tokens))
	q.Set("maximum_number_of_snippets", strconv.Itoa(minInt(maxInt(req.NumResults*4, 10), 100)))
	if req.Depth == DepthDeep {
		q.Set("context_threshold_mode", "balanced")
	} else {
		q.Set("context_threshold_mode", "lenient")
	}
	q.Set("enable_source_metadata", "true")
	u.RawQuery = q.Encode()
	headers := commonHeaders()
	headers["x-subscription-token"] = key
	var payload braveContextResponse
	if err := doJSON(ctx, Brave, http.MethodGet, u.String(), headers, nil, &payload); err != nil {
		return base, nil, err
	}
	all := append([]braveContextItem{}, payload.Grounding.Generic...)
	all = append(all, payload.Grounding.Map...)
	if payload.Grounding.POI != nil {
		all = append(all, *payload.Grounding.POI)
	}
	items := make([]ResultItem, 0, len(all))
	for _, r := range all {
		u := safeURL(r.URL)
		if u == "" {
			continue
		}
		source := payload.Sources[u]
		items = append(items, ResultItem{Title: firstNonEmpty(cleanText(r.Title), cleanText(r.Name), cleanText(source.Title), u), URL: u, Snippet: limit(strings.Join(cleanStringArray(r.Snippets), " "), 4000), PublishedDate: bravePublishedDate(source.Age)})
	}
	return base, items, nil
}

func cleanText(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func cleanStringArray(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		if stringsSlice, ok := v.([]string); ok {
			return stringsSlice
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := cleanText(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func safeURL(v any) string {
	text := cleanText(v)
	if text == "" {
		return ""
	}
	u, err := url.Parse(text)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

func limit(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	return strings.TrimSpace(text[:max-1]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func dedupe(items []ResultItem, limitN int) []ResultItem {
	seen := map[string]bool{}
	out := make([]ResultItem, 0, minInt(limitN, len(items)))
	for _, item := range items {
		if item.URL == "" || seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		if item.Title == "" {
			item.Title = item.URL
		}
		item.Snippet = limit(cleanText(item.Snippet), 4000)
		out = append(out, item)
		if len(out) >= limitN {
			break
		}
	}
	return out
}

func formatResults(id ProviderID, query string, items []ResultItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Motor: %s\nConsulta: %s\n\n", Labels[id], query)
	for i, item := range items {
		fmt.Fprintf(&b, "%d. [%s](%s)\n", i+1, item.Title, item.URL)
		meta := strings.Join(nonEmpty(item.PublishedDate, item.Author), " · ")
		if meta != "" {
			fmt.Fprintf(&b, "   %s\n", meta)
		}
		if item.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", item.Snippet)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func redactSecret(message, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Stable provider ordering is useful in tests and settings UIs.
func SortedAvailable(settings Settings, auth Auth) []ProviderID {
	ids := AvailableOrder(settings, auth)
	sort.SliceStable(ids, func(i, j int) bool { return string(ids[i]) < string(ids[j]) })
	return ids
}
