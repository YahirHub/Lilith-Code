package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	tagPattern    = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>`)
	anyTagPattern = regexp.MustCompile(`<[^>]+>`)
	blankPattern  = regexp.MustCompile(`\n{3,}`)
)

func init() {
	register(Definition{
		Name:        "read_url",
		Description: "Fetch a public URL and return its plain text (useful for online documentation).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "Full http(s) URL."},
			},
			"required": []string{"url"},
		},
		Run: func(ctx context.Context, args map[string]any, _ Env) (string, error) {
			url := strings.TrimSpace(str(args, "url"))
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				return "", fmt.Errorf("invalid URL: %s", url)
			}
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(runCtx, http.MethodGet, url, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "lilith-cli")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return "", fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileBytes))
			if err != nil {
				return "", err
			}
			text := string(body)
			if strings.Contains(resp.Header.Get("Content-Type"), "html") {
				text = tagPattern.ReplaceAllString(text, " ")
				text = anyTagPattern.ReplaceAllString(text, " ")
			}
			text = blankPattern.ReplaceAllString(strings.TrimSpace(text), "\n\n")
			return text, nil
		},
	})
}
