// Package openai — ChatGPT/Codex OAuth login flow.
//
// Implementa dos flujos oficiales compatibles con Codex:
//   - Callback de navegador con PKCE en http://localhost:1455/auth/callback.
//   - Device code para entornos sin navegador.
//
// Los tokens se guardan en internal/secrets bajo el provider id "openai-codex".
package openai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lilith/li/internal/secrets"
)

// Constantes OAuth (mismo cliente oficial usado por Codex CLI).
const (
	ChatGPTCodexProviderID   = "openai-codex"
	ChatGPTCodexProviderName = "ChatGPT Codex"

	ChatGPTOAuthClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	ChatGPTOAuthAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	ChatGPTOAuthTokenURL     = "https://auth.openai.com/oauth/token"
	ChatGPTOAuthScope        = "openid profile email offline_access api.connectors.read api.connectors.invoke"

	ChatGPTDeviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	ChatGPTDeviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	ChatGPTDeviceVerificationURL = "https://auth.openai.com/codex/device"
	ChatGPTDeviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"

	ChatGPTBackendBaseURL = "https://chatgpt.com/backend-api/codex"

	oauthCallbackPath = "/auth/callback"
	oauthPrimaryPort  = 1455
	oauthFallbackPort = 1457
	oauthUserAgent    = "lilith-cli/0.1"
	callbackTimeout   = 5 * time.Minute
	deviceCodeTimeout = 15 * time.Minute
	defaultDevicePoll = 5 * time.Second
)

// CodexModels es el catálogo por defecto expuesto cuando la sesión Codex está
// activa. Sigue la lista de modelos publicada por el flujo Codex oficial.
var CodexModels = []struct{ ID, Name string }{
	{"gpt-5.6-sol", "GPT-5.6 Sol"},
	{"gpt-5.6-terra", "GPT-5.6 Terra"},
	{"gpt-5.6-luna", "GPT-5.6 Luna"},
	{"gpt-5.5", "GPT-5.5"},
	{"gpt-5.4", "GPT-5.4"},
	{"gpt-5.4-mini", "GPT-5.4 mini"},
	{"gpt-5.3-codex-spark", "GPT-5.3 Codex Spark"},
}

// OAuthFlow inicia el flujo de callback local. Devuelve la URL a abrir y un
// canal donde se entregarán los tokens (o un error) cuando el usuario complete
// la autorización.
type OAuthFlow struct {
	AuthURL     string
	RedirectURI string

	verifier string
	state    string
	server   *http.Server
	result   chan oauthResult
}

type oauthResult struct {
	tokens secrets.OAuthTokens
	err    error
}

// StartBrowserFlow arranca el servidor local y devuelve la URL a visitar.
func StartBrowserFlow() (*OAuthFlow, error) {
	ln, redirectURI, err := listenOAuthCallback()
	if err != nil {
		return nil, err
	}

	verifier, err := randomBase64URL(32)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	state, err := randomBase64URL(16)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", ChatGPTOAuthClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("scope", ChatGPTOAuthScope)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "codex_cli_rs")

	flow := &OAuthFlow{
		AuthURL:     ChatGPTOAuthAuthorizeURL + "?" + q.Encode(),
		RedirectURI: redirectURI,
		verifier:    verifier,
		state:       state,
		result:      make(chan oauthResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(oauthCallbackPath, flow.handleCallback)
	flow.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = flow.server.Serve(ln) }()
	return flow, nil
}

func listenOAuthCallback() (net.Listener, string, error) {
	var errs []string
	for _, port := range []int{oauthPrimaryPort, oauthFallbackPort} {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, fmt.Sprintf("http://localhost:%d%s", port, oauthCallbackPath), nil
		}
		errs = append(errs, fmt.Sprintf("%d: %v", port, err))
	}
	return nil, "", fmt.Errorf("no se pudo abrir el callback local en los puertos oficiales de Codex (%s)", strings.Join(errs, "; "))
}

func (f *OAuthFlow) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		writePage(w, false, "No se recibió un código de autorización.")
		f.deliver(oauthResult{err: errors.New("callback sin código")})
		return
	}
	if state != f.state {
		writePage(w, false, "El estado OAuth no coincide.")
		f.deliver(oauthResult{err: errors.New("estado OAuth no coincide")})
		return
	}
	tok, err := exchangeCode(r.Context(), code, f.verifier, f.RedirectURI)
	if err != nil {
		writePage(w, false, err.Error())
		f.deliver(oauthResult{err: err})
		return
	}
	writePage(w, true, "")
	f.deliver(oauthResult{tokens: tok})
}

func (f *OAuthFlow) deliver(r oauthResult) {
	select {
	case f.result <- r:
	default:
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = f.server.Close()
	}()
}

// Wait bloquea hasta recibir tokens, error o expirar el timeout.
func (f *OAuthFlow) Wait(ctx context.Context) (secrets.OAuthTokens, error) {
	select {
	case r := <-f.result:
		return r.tokens, r.err
	case <-time.After(callbackTimeout):
		_ = f.server.Close()
		return secrets.OAuthTokens{}, errors.New("expiró la espera del callback OAuth")
	case <-ctx.Done():
		_ = f.server.Close()
		return secrets.OAuthTokens{}, ctx.Err()
	}
}

// Close libera el puerto si el flujo se cancela.
func (f *OAuthFlow) Close() {
	if f.server != nil {
		_ = f.server.Close()
	}
}

// DeviceCodeInfo describe el código que el usuario debe introducir.
type DeviceCodeInfo struct {
	VerificationURL string
	UserCode        string
	IntervalMs      int
	deviceAuthID    string
}

// RequestDeviceCode solicita un código de dispositivo a OpenAI.
func RequestDeviceCode(ctx context.Context) (*DeviceCodeInfo, error) {
	payload, _ := json.Marshal(map[string]string{"client_id": ChatGPTOAuthClientID})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ChatGPTDeviceUserCodeURL, strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("el acceso por código de dispositivo no está habilitado para esta cuenta")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("device code HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     any    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.DeviceAuthID == "" || out.UserCode == "" {
		return nil, errors.New("respuesta device code incompleta")
	}
	interval := devicePollIntervalMs(out.Interval)
	if interval < 1000 {
		interval = int(defaultDevicePoll / time.Millisecond)
	}
	return &DeviceCodeInfo{
		VerificationURL: ChatGPTDeviceVerificationURL,
		UserCode:        out.UserCode,
		IntervalMs:      interval,
		deviceAuthID:    out.DeviceAuthID,
	}, nil
}

// PollDeviceCode espera a que el usuario complete la autorización en el
// navegador y devuelve los tokens correspondientes.
func PollDeviceCode(ctx context.Context, info *DeviceCodeInfo) (secrets.OAuthTokens, error) {
	deadline := time.Now().Add(deviceCodeTimeout)
	interval := time.Duration(info.IntervalMs) * time.Millisecond
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return secrets.OAuthTokens{}, ctx.Err()
		case <-time.After(interval):
		}
		body, _ := json.Marshal(map[string]string{
			"device_auth_id": info.deviceAuthID,
			"user_code":      info.UserCode,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ChatGPTDeviceTokenURL, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", oauthUserAgent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			var payload struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				resp.Body.Close()
				return secrets.OAuthTokens{}, err
			}
			resp.Body.Close()
			if payload.AuthorizationCode == "" || payload.CodeVerifier == "" {
				return secrets.OAuthTokens{}, errors.New("device code sin credenciales completas")
			}
			return exchangeCode(ctx, payload.AuthorizationCode, payload.CodeVerifier, ChatGPTDeviceRedirectURI)
		}
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			continue
		}
		code := parseErrorCode(bodyBytes)
		if code == "deviceauth_authorization_pending" {
			continue
		}
		if code == "slow_down" {
			interval += 5 * time.Second
			continue
		}
		return secrets.OAuthTokens{}, fmt.Errorf("device auth HTTP %d", resp.StatusCode)
	}
	return secrets.OAuthTokens{}, errors.New("expiró el código de dispositivo")
}

// RefreshTokens intercambia un refresh_token por un access_token nuevo.
func RefreshTokens(ctx context.Context, refresh string) (secrets.OAuthTokens, error) {
	if refresh == "" {
		return secrets.OAuthTokens{}, errors.New("no hay refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", ChatGPTOAuthClientID)
	form.Set("refresh_token", refresh)
	return postToken(ctx, form)
}

func exchangeCode(ctx context.Context, code, verifier, redirect string) (secrets.OAuthTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ChatGPTOAuthClientID)
	form.Set("redirect_uri", redirect)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	return postToken(ctx, form)
}

func postToken(ctx context.Context, form url.Values) (secrets.OAuthTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatGPTOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return secrets.OAuthTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return secrets.OAuthTokens{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return secrets.OAuthTokens{}, fmt.Errorf("token HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return secrets.OAuthTokens{}, err
	}
	if out.AccessToken == "" {
		return secrets.OAuthTokens{}, errors.New("respuesta sin access_token")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	return secrets.OAuthTokens{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).Unix(),
		TokenType:    out.TokenType,
	}, nil
}

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func parseErrorCode(body []byte) string {
	var payload struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	switch v := payload.Error.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["code"].(string); ok {
			return s
		}
	}
	return ""
}

func devicePollIntervalMs(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v * 1000)
	case string:
		seconds, err := time.ParseDuration(v + "s")
		if err == nil {
			return int(seconds / time.Millisecond)
		}
	}
	return int(defaultDevicePoll / time.Millisecond)
}

func writePage(w http.ResponseWriter, ok bool, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>Lilith · Conectado</title>
<body style="background:#0a0a0a;color:#e5e5e5;font-family:system-ui,sans-serif;display:flex;min-height:100vh;justify-content:center;align-items:center;margin:0">
<div style="text-align:center"><h1 style="color:#4ade80">Conectado con ChatGPT</h1><p>Puedes cerrar esta pestaña y volver a Lilith.</p></div>`)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Lilith · Error</title>
<body style="background:#0a0a0a;color:#e5e5e5;font-family:system-ui,sans-serif;display:flex;min-height:100vh;justify-content:center;align-items:center;margin:0">
<div style="text-align:center"><h1 style="color:#f87171">Falló la conexión</h1><p>%s</p></div>`, escapeHTML(msg))
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", `'`, "&#39;")
	return r.Replace(s)
}

// SaveTokens persiste tokens OAuth para el proveedor Codex.
func SaveTokens(dir string, tok secrets.OAuthTokens) error {
	st, err := secrets.Load(dir)
	if err != nil {
		return err
	}
	st.OAuth[ChatGPTCodexProviderID] = tok
	return secrets.Save(dir, st)
}

// LoadTokens recupera tokens OAuth guardados (si existen).
func LoadTokens(dir string) (secrets.OAuthTokens, bool, error) {
	st, err := secrets.Load(dir)
	if err != nil {
		return secrets.OAuthTokens{}, false, err
	}
	tok, ok := st.OAuth[ChatGPTCodexProviderID]
	return tok, ok, nil
}
