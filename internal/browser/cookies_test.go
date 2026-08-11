package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
)

func TestLoadCookieJSONSupportsCookieEditorArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	data := `[
		{"domain":".example.com","expirationDate":1893456000.5,"hostOnly":false,"httpOnly":true,"name":"session","path":"/","sameSite":"no_restriction","secure":true,"session":false,"value":"top-secret"},
		{"domain":"example.com","name":"pref","path":"/app","sameSite":"lax","secure":false,"value":"dark"}
	]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cookies, skipped, err := loadCookieJSON(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(cookies) != 2 {
		t.Fatalf("resultado inesperado: cookies=%d skipped=%d", len(cookies), skipped)
	}
	if cookies[0].SameSite != network.CookieSameSiteNone || !cookies[0].Secure || !cookies[0].HTTPOnly {
		t.Fatalf("atributos de cookie no preservados: %#v", cookies[0])
	}
	if cookies[0].Expires == nil {
		t.Fatal("expirationDate no fue convertida")
	}
	got := cookies[0].Expires.Time()
	want := time.Unix(1893456000, 500_000_000).UTC()
	if !got.Equal(want) {
		t.Fatalf("expiración=%v want=%v", got, want)
	}
}

func TestLoadCookieJSONSupportsWrappedCookiesAndDefaultURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	data := `{"cookies":[{"name":"host-session","value":"secret","httpOnly":true,"sameSite":"strict"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cookies, skipped, err := loadCookieJSON(path, "https://panel.example.test/login")
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(cookies) != 1 {
		t.Fatalf("resultado inesperado: cookies=%d skipped=%d", len(cookies), skipped)
	}
	if cookies[0].URL != "https://panel.example.test/login" || cookies[0].SameSite != network.CookieSameSiteStrict {
		t.Fatalf("cookie inesperada: %#v", cookies[0])
	}
}

func TestLoadCookieJSONNeverEmbedsValuesInErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	secret := "VERY-SENSITIVE-COOKIE-VALUE"
	data := `[{"name":"broken","value":"` + secret + `",]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCookieJSON(path, "")
	if err == nil {
		t.Fatal("se esperaba que no hubiera cookies importables")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("el valor sensible apareció en el error: %v", err)
	}
}

func TestCookieArrayFromJSONRejectsUnknownObject(t *testing.T) {
	_, err := cookieArrayFromJSON([]byte(`{"items":[]}`))
	if err == nil {
		t.Fatal("se esperaba error para un formato desconocido")
	}
}

func TestNormalizeCookieRejectsInsecureSameSiteNoneAndPartitioned(t *testing.T) {
	if _, ok := normalizeCookie(map[string]any{
		"name": "session", "value": "secret", "domain": "example.com", "sameSite": "none", "secure": false,
	}, ""); ok {
		t.Fatal("SameSite=None sin Secure no debe importarse")
	}
	if _, ok := normalizeCookie(map[string]any{
		"name": "partitioned", "value": "secret", "domain": "example.com", "secure": true,
		"partitionKey": map[string]any{"topLevelSite": "https://example.com"},
	}, ""); ok {
		t.Fatal("una cookie particionada no debe aplanarse silenciosamente")
	}
}

func TestCookieImportReportContainsOnlyCounters(t *testing.T) {
	report := CookieImportReport{Imported: 3, Skipped: 1}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if got != `{"imported":3,"skipped":1}` {
		t.Fatalf("reporte inesperado: %s", got)
	}
}

func TestLoadCookieJSONDoesNotExposeResolvedPathInErrors(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "private-user", "cookies.json")
	_, _, err := loadCookieJSON(secretPath, "")
	if err == nil {
		t.Fatal("se esperaba error para un archivo inexistente")
	}
	if strings.Contains(err.Error(), secretPath) || strings.Contains(err.Error(), "private-user") {
		t.Fatalf("el error filtró la ruta local: %v", err)
	}
}

func TestNormalizeCookiePreservesHostOnlyScope(t *testing.T) {
	cookie, ok := normalizeCookie(map[string]any{
		"name": "session", "value": "secret", "domain": "example.com", "hostOnly": true,
		"path": "/", "secure": true,
	}, "")
	if !ok {
		t.Fatal("cookie host-only válida fue rechazada")
	}
	if cookie.Domain != "" || cookie.URL != "https://example.com/" {
		t.Fatalf("scope host-only no preservado: %#v", cookie)
	}

	domainCookie, ok := normalizeCookie(map[string]any{
		"name": "shared", "value": "secret", "domain": "example.com", "hostOnly": false,
		"path": "/", "secure": true,
	}, "")
	if !ok || domainCookie.Domain != "example.com" || domainCookie.URL != "" {
		t.Fatalf("cookie de dominio inesperada: %#v ok=%v", domainCookie, ok)
	}
}

func TestNormalizeCookieEnforcesSecureAndHostPrefixes(t *testing.T) {
	valid, ok := normalizeCookie(map[string]any{
		"name": "__Host-session", "value": "secret", "domain": "example.com", "hostOnly": true,
		"path": "/", "secure": true,
	}, "")
	if !ok || valid.Domain != "" || valid.URL != "https://example.com/" {
		t.Fatalf("cookie __Host válida fue rechazada/degradada: %#v ok=%v", valid, ok)
	}
	for _, raw := range []map[string]any{
		{"name": "__Secure-session", "value": "secret", "domain": "example.com", "hostOnly": true, "secure": false},
		{"name": "__Host-session", "value": "secret", "domain": ".example.com", "hostOnly": false, "path": "/", "secure": true},
		{"name": "__Host-session", "value": "secret", "domain": "example.com", "hostOnly": true, "path": "/app", "secure": true},
	} {
		if _, ok := normalizeCookie(raw, ""); ok {
			t.Fatalf("cookie con prefijo inválido fue aceptada: %#v", raw)
		}
	}
}

func TestImportCookiesRequiresActiveTab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(path, []byte(`[{"name":"session","value":"secret","domain":"example.com","hostOnly":true,"path":"/","secure":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	session := &Session{tabs: map[string]*Tab{}}
	_, err := session.ImportCookies(context.Background(), path, "")
	if err == nil {
		t.Fatal("se esperaba error al importar cookies sin una pestaña activa")
	}
	if !strings.Contains(err.Error(), "pestaña activa") {
		t.Fatalf("error inesperado: %v", err)
	}
}
