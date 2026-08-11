package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
)

const (
	maxCookieJSONBytes = 16 << 20
	maxCookieCount     = 10000
)

type CookieImportReport struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

type importedCookie struct {
	Name     string
	Value    string
	URL      string
	Domain   string
	Path     string
	Secure   bool
	HTTPOnly bool
	SameSite network.CookieSameSite
	Expires  *cdp.TimeSinceEpoch
}

func (s *Session) ImportCookies(ctx context.Context, path, defaultURL string) (CookieImportReport, error) {
	cookies, skipped, err := loadCookieJSON(path, defaultURL)
	if err != nil {
		return CookieImportReport{}, err
	}
	if len(cookies) == 0 {
		return CookieImportReport{Skipped: skipped}, errors.New("el JSON no contiene cookies importables")
	}
	tab, err := s.CurrentTab()
	if err != nil {
		return CookieImportReport{}, err
	}
	params := make([]*network.CookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		params = append(params, &network.CookieParam{
			Name: cookie.Name, Value: cookie.Value, URL: cookie.URL, Domain: cookie.Domain, Path: cookie.Path,
			Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite, Expires: cookie.Expires,
		})
	}
	if err := runTargetCommand(ctx, tab.ctx, 30*time.Second, func(cmdCtx context.Context) error {
		return network.SetCookies(params).Do(cmdCtx)
	}); err != nil {
		// Do not propagate protocol details here: cookie values are part of the
		// request payload and must never reach model-visible error text.
		return CookieImportReport{}, errors.New("importar cookies mediante CDP: el navegador rechazó una o más cookies")
	}
	return CookieImportReport{Imported: len(cookies), Skipped: skipped}, nil
}

func loadCookieJSON(path, defaultURL string) ([]importedCookie, int, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, 0, errors.New("cookie_path es obligatorio")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, errors.New("archivo JSON de cookies no encontrado")
		}
		return nil, 0, errors.New("no se pudo abrir el archivo JSON de cookies")
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("cookie_path debe apuntar a un archivo regular")
	}
	if info.Size() > maxCookieJSONBytes {
		return nil, 0, fmt.Errorf("JSON de cookies demasiado grande: máximo %d MiB", maxCookieJSONBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, errors.New("no se pudo leer el archivo JSON de cookies")
	}
	rawCookies, err := cookieArrayFromJSON(data)
	if err != nil {
		return nil, 0, err
	}
	if len(rawCookies) > maxCookieCount {
		return nil, 0, fmt.Errorf("demasiadas cookies: máximo %d", maxCookieCount)
	}
	defaultURL = strings.TrimSpace(defaultURL)
	if defaultURL != "" {
		if parsed, parseErr := url.Parse(defaultURL); parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			defaultURL = ""
		}
	}
	cookies := make([]importedCookie, 0, len(rawCookies))
	skipped := 0
	for _, raw := range rawCookies {
		cookie, ok := normalizeCookie(raw, defaultURL)
		if !ok {
			skipped++
			continue
		}
		cookies = append(cookies, cookie)
	}
	return cookies, skipped, nil
}

func cookieArrayFromJSON(data []byte) ([]map[string]any, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("JSON de cookies vacío")
	}
	var array []map[string]any
	if data[0] == '[' {
		if err := json.Unmarshal(data, &array); err != nil {
			return nil, fmt.Errorf("JSON de cookies inválido: %w", err)
		}
		return array, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("JSON de cookies inválido: %w", err)
	}
	for _, key := range []string{"cookies", "Cookies"} {
		if raw, ok := object[key]; ok {
			if err := json.Unmarshal(raw, &array); err != nil {
				return nil, fmt.Errorf("campo %s inválido en JSON de cookies: %w", key, err)
			}
			return array, nil
		}
	}
	return nil, errors.New("formato JSON no reconocido: se esperaba un array o un objeto con campo cookies")
}

func normalizeCookie(raw map[string]any, defaultURL string) (importedCookie, bool) {
	if hasNonNilField(raw, "partitionKey", "partition_key", "partitionKeyOpaque") {
		return importedCookie{}, false
	}
	name, ok := stringField(raw, "name", "Name")
	if !ok || strings.TrimSpace(name) == "" {
		return importedCookie{}, false
	}
	value, ok := stringField(raw, "value", "Value")
	if !ok {
		return importedCookie{}, false
	}
	domain, _ := stringField(raw, "domain", "Domain", "host")
	domain = strings.TrimSpace(domain)
	cookieURL, _ := stringField(raw, "url", "URL")
	cookieURL = strings.TrimSpace(cookieURL)
	secure, _ := boolField(raw, "secure", "Secure")
	hostOnly, hostOnlySet := boolField(raw, "hostOnly", "host_only", "HostOnly")

	// Extension exports commonly carry both domain and hostOnly. CDP's Domain
	// field creates a domain-scoped cookie, so a host-only cookie must instead
	// be associated through URL while leaving Domain empty. When hostOnly is not
	// present, a domain without a leading dot is treated as host-only because
	// that matches the most common Cookie-Editor/Playwright exports.
	if cookieURL == "" && domain != "" && (hostOnly || (!hostOnlySet && !strings.HasPrefix(domain, "."))) {
		var ok bool
		cookieURL, ok = cookieURLForHost(domain, secure)
		if !ok {
			return importedCookie{}, false
		}
		domain = ""
	}
	if cookieURL == "" && domain == "" {
		cookieURL = defaultURL
	}
	if cookieURL == "" && domain == "" {
		return importedCookie{}, false
	}
	if cookieURL != "" && !isCookieURL(cookieURL) {
		return importedCookie{}, false
	}
	path, _ := stringField(raw, "path", "Path")
	if path == "" {
		path = "/"
	}
	httpOnly, _ := boolField(raw, "httpOnly", "http_only", "HttpOnly")
	sameSiteRaw, _ := stringField(raw, "sameSite", "same_site", "SameSite")
	sameSite, sameSiteOK := normalizeSameSite(sameSiteRaw)
	if !sameSiteOK || (sameSite == network.CookieSameSiteNone && !secure) {
		return importedCookie{}, false
	}
	if strings.HasPrefix(name, "__Secure-") && !secure {
		return importedCookie{}, false
	}
	if strings.HasPrefix(name, "__Host-") && (!secure || path != "/" || domain != "" || cookieURL == "") {
		return importedCookie{}, false
	}
	expires := expirationField(raw)
	return importedCookie{
		Name: strings.TrimSpace(name), Value: value, URL: strings.TrimSpace(cookieURL), Domain: strings.TrimSpace(domain),
		Path: path, Secure: secure, HTTPOnly: httpOnly, SameSite: sameSite, Expires: expires,
	}, true
}

func cookieURLForHost(domain string, secure bool) (string, bool) {
	host := strings.TrimSpace(strings.TrimPrefix(domain, "."))
	if host == "" || strings.ContainsAny(host, `/\ @`) {
		return "", false
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	value := (&url.URL{Scheme: scheme, Host: host, Path: "/"}).String()
	if !isCookieURL(value) {
		return "", false
	}
	return value, true
}

func isCookieURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func normalizeSameSite(value string) (network.CookieSameSite, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unspecified", "default", "null":
		return "", true
	case "strict":
		return network.CookieSameSiteStrict, true
	case "lax":
		return network.CookieSameSiteLax, true
	case "none", "no_restriction", "no restriction":
		return network.CookieSameSiteNone, true
	default:
		return "", false
	}
}

func expirationField(raw map[string]any) *cdp.TimeSinceEpoch {
	for _, key := range []string{"expirationDate", "expiration_date", "expires", "expiry", "ExpirationDate"} {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		seconds, ok := unixSeconds(value)
		if !ok || seconds <= 0 {
			continue
		}
		whole, frac := math.Modf(seconds)
		t := time.Unix(int64(whole), int64(frac*float64(time.Second))).UTC()
		converted := cdp.TimeSinceEpoch(t)
		return &converted
	}
	return nil
}

func unixSeconds(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return float64(parsed.UnixNano()) / float64(time.Second), true
		}
	}
	return 0, false
}

func hasNonNilField(raw map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := raw[key]; ok && value != nil {
			return true
		}
	}
	return false
}

func stringField(raw map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			return text, true
		}
	}
	return "", false
}

func boolField(raw map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if result, ok := value.(bool); ok {
			return result, true
		}
	}
	return false, false
}
