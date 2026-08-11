package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	libbrowser "github.com/lilith/li/internal/browser"
	"github.com/lilith/li/internal/interaction"
)

func init() {
	register(Definition{
		Name: "browser",
		Description: "Open and persistently control Chrome, Chromium, Edge, Brave or another CDP-compatible browser. " +
			"Discover installed/running browsers and local profiles, use isolated temporary/persistent profiles or an explicitly selected existing CDP profile, import exported cookies JSON locally, navigate, inspect compact DOM snapshots, interact with forms, debug console/network/JavaScript, manage tabs and capture screenshots. " +
			"Use fill_secret for passwords so secrets never enter model-visible tool arguments.",
		PromptSnippet: "Persistent token-efficient browser automation and DevTools debugging",
		PromptGuidelines: []string{
			"Prefer browser snapshot with delta=true after the first snapshot; interact by returned refs instead of repeatedly requesting full HTML.",
			"Prefer profile_mode=persistent when login state must survive Lilith restarts; cookie_path can seed that isolated profile from an exported JSON without exposing cookie values to the model.",
			"Use action=profiles before profile_mode=existing. Attach to an existing personal profile only when the user explicitly wants it and the browser exposes a reusable CDP endpoint.",
			"Cookie JSON must stay file-backed: pass cookie_path only; never read, echo, summarize or place cookie values in model-visible arguments or output.",
			"Use fill_secret for passwords, tokens and other sensitive form values; never place secrets in type/fill arguments.",
			"Inspect console and network errors before changing frontend code, and verify the result with a fresh delta snapshot.",
			"For broad multi-page frontend regression audits, prefer delegating to the isolated frontend-browser-auditor agent when available; keep only actionable findings in the parent context.",
			"After navigation or reload, call scripts again before search_source because CDP script IDs belong to the current document.",
			"scripts verifies script_id-to-URL mappings by source hash by default; use verify=false only when a faster unverified inventory is explicitly preferable.",
		},
		Parameters: browserParameters(),
		Mutating:   true,
		Available: func(env Env) bool {
			return strings.TrimSpace(env.ConfigDir) != ""
		},
		Run: runBrowser,
	})
}

func browserParameters() map[string]any {
	actions := []string{
		"discover", "profiles", "get_default", "set_default", "start", "list_sessions", "status", "close", "close_all",
		"navigate", "back", "forward", "reload", "snapshot", "text", "html", "click", "type", "fill", "fill_secret",
		"select", "key", "wait", "evaluate", "screenshot", "console", "network", "response_body",
		"tabs", "new_tab", "switch_tab", "close_tab", "scripts", "search_source", "performance", "import_cookies",
	}
	properties := map[string]any{
		"action":            map[string]any{"type": "string", "enum": actions},
		"session_id":        map[string]any{"type": "string", "description": "Stable logical browser session ID."},
		"candidate_id":      map[string]any{"type": "string"},
		"executable":        map[string]any{"type": "string", "description": "Explicit Chrome/Chromium-compatible executable."},
		"remote_url":        map[string]any{"type": "string", "description": "Explicit CDP HTTP or WebSocket endpoint."},
		"headless":          map[string]any{"type": "boolean", "description": "true for hidden operation; false for a visible browser window."},
		"profile_mode":      map[string]any{"type": "string", "enum": []string{"temporary", "persistent", "custom", "existing"}},
		"profile_name":      map[string]any{"type": "string", "description": "Dedicated Lilith profile name for persistent mode."},
		"profile_id":        map[string]any{"type": "string", "description": "Existing browser profile ID returned by action=profiles."},
		"profile_directory": map[string]any{"type": "string", "description": "Chromium profile subdirectory such as Default or Profile 1."},
		"user_data_dir":     map[string]any{"type": "string", "description": "Browser user data directory. For existing mode this may be a discovered browser data root."},
		"cookie_path":       map[string]any{"type": "string", "description": "Path to an exported cookies JSON file. Values are read locally and never returned to the model."},
		"url":               map[string]any{"type": "string"},
		"tab_id":            map[string]any{"type": "string"},
		"ref":               map[string]any{"type": "string", "description": "Compact element reference from snapshot, e.g. e4."},
		"selector":          map[string]any{"type": "string", "description": "CSS selector; prefer ref from snapshot."},
		"value":             map[string]any{"type": "string", "description": "Non-secret value. Use fill_secret for sensitive data."},
		"append":            map[string]any{"type": "boolean"},
		"key":               map[string]any{"type": "string"},
		"expression":        map[string]any{"type": "string"},
		"path":              map[string]any{"type": "string", "description": "Screenshot output path, relative to the project when not absolute."},
		"full_page":         map[string]any{"type": "boolean"},
		"quality":           map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"delta":             map[string]any{"type": "boolean"},
		"max_text":          map[string]any{"type": "integer", "minimum": 500, "maximum": 50000},
		"max_elements":      map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		"max_bytes":         map[string]any{"type": "integer", "minimum": 100, "maximum": 1000000},
		"limit":             map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"clear":             map[string]any{"type": "boolean"},
		"errors_only":       map[string]any{"type": "boolean"},
		"request_id":        map[string]any{"type": "string"},
		"script_id":         map[string]any{"type": "string", "description": "Ephemeral CDP script ID returned by scripts for the active tab and current document. Refresh scripts after navigation or reload."},
		"query":             map[string]any{"type": "string"},
		"case_sensitive":    map[string]any{"type": "boolean"},
		"verify":            map[string]any{"type": "boolean", "description": "For scripts, verify script_id-to-URL mappings against the SHA-256 hash of the current source. Defaults to true."},
		"timeout_ms":        map[string]any{"type": "integer", "minimum": 0, "maximum": 120000},
		"secret_label":      map[string]any{"type": "string", "description": "Human-readable account/site label shown only in the local secret prompt."},
	}
	sessionActions := []string{
		"status", "close", "navigate", "back", "forward", "reload", "snapshot", "text", "html", "click", "type", "fill", "fill_secret",
		"select", "key", "wait", "evaluate", "screenshot", "console", "network", "response_body", "tabs", "new_tab", "switch_tab", "close_tab",
		"scripts", "search_source", "performance", "import_cookies",
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": []string{"action"},
		"allOf": []any{
			browserCondition(sessionActions, []string{"session_id"}),
			browserCondition([]string{"navigate"}, []string{"url"}),
			browserCondition([]string{"type", "fill", "select"}, []string{"value"}),
			browserCondition([]string{"key"}, []string{"key"}),
			browserCondition([]string{"evaluate"}, []string{"expression"}),
			browserCondition([]string{"screenshot"}, []string{"path"}),
			browserCondition([]string{"response_body"}, []string{"request_id"}),
			browserCondition([]string{"switch_tab"}, []string{"tab_id"}),
			browserCondition([]string{"search_source"}, []string{"script_id", "query"}),
			browserCondition([]string{"import_cookies"}, []string{"cookie_path"}),
		},
	}
}

func browserCondition(actions, required []string) map[string]any {
	return map[string]any{
		"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"enum": actions}}, "required": []string{"action"}},
		"then": map[string]any{"required": required},
	}
}

func runBrowser(ctx context.Context, args map[string]any, env Env) (string, error) {
	action := strings.TrimSpace(str(args, "action"))
	if action == "" {
		return "", errors.New("action is required")
	}
	manager := libbrowser.GetManager(env.ConfigDir)
	switch action {
	case "discover":
		candidates, err := libbrowser.Discover(ctx)
		if err != nil {
			return "", err
		}
		cfg, _ := libbrowser.LoadConfig(env.ConfigDir)
		return jsonOutput(map[string]any{
			"action": "discover", "candidates": candidates, "count": len(candidates), "default": cfg,
			"recommendation": browserRecommendation(candidates),
		})
	case "profiles":
		profiles := libbrowser.DiscoverProfiles(ctx)
		return jsonOutput(map[string]any{"action": action, "profiles": profiles, "count": len(profiles)})
	case "get_default":
		cfg, err := libbrowser.LoadConfig(env.ConfigDir)
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "default": cfg})
	case "set_default":
		current, _ := libbrowser.LoadConfig(env.ConfigDir)
		headless := current.Headless
		if _, ok := args["headless"]; ok {
			headless = boolArgOr(args, "headless", true)
		}
		cfg, err := manager.SetDefault(ctx,
			str(args, "candidate_id"), str(args, "executable"), str(args, "remote_url"), headless,
			libbrowser.ProfileMode(str(args, "profile_mode")), str(args, "profile_name"), str(args, "profile_id"),
			str(args, "profile_directory"), str(args, "user_data_dir"),
		)
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "default": cfg})
	case "start":
		cfg, _ := libbrowser.LoadConfig(env.ConfigDir)
		headless := cfg.Headless
		if _, ok := args["headless"]; ok {
			headless = boolArgOr(args, "headless", true)
		}
		startURL := str(args, "url")
		cookiePath := resolveBrowserImportPath(str(args, "cookie_path"), env.Root)
		opts := libbrowser.StartOptions{
			SessionID: str(args, "session_id"), CandidateID: str(args, "candidate_id"),
			Executable: str(args, "executable"), RemoteURL: str(args, "remote_url"), Headless: headless,
			ProfileMode: libbrowser.ProfileMode(str(args, "profile_mode")), ProfileName: str(args, "profile_name"),
			ProfileID: str(args, "profile_id"), ProfileDirectory: str(args, "profile_directory"),
			UserDataDir: str(args, "user_data_dir"), StartURL: startURL,
		}
		if cookiePath != "" {
			opts.StartURL = ""
		}
		info, err := manager.StartDefault(ctx, opts)
		if err != nil {
			return "", err
		}
		var cookieReport any
		if cookiePath != "" {
			session, sessionErr := manager.Session(info.ID)
			if sessionErr != nil {
				_ = manager.Close(info.ID)
				return "", sessionErr
			}
			report, importErr := session.ImportCookies(ctx, cookiePath, startURL)
			if importErr != nil {
				_ = manager.Close(info.ID)
				return "", importErr
			}
			cookieReport = report
			if startURL != "" {
				if navErr := session.Navigate(ctx, startURL); navErr != nil {
					_ = manager.Close(info.ID)
					return "", navErr
				}
			}
			info = session.Info()
		}
		out := map[string]any{
			"ok": true, "action": action, "session": info,
			"next": "Usa snapshot con session_id; después interactúa con refs y solicita delta=true para ahorrar tokens.",
		}
		if cookieReport != nil {
			out["cookies"] = cookieReport
		}
		return jsonOutput(out)
	case "list_sessions":
		values := manager.List()
		return jsonOutput(map[string]any{"action": action, "sessions": values, "count": len(values)})
	case "close_all":
		manager.CloseAll()
		return jsonOutput(map[string]any{"ok": true, "action": action})
	}

	session, err := manager.Session(str(args, "session_id"))
	if err != nil {
		return "", err
	}
	ref, selector := str(args, "ref"), str(args, "selector")
	resolveSelector := func(required bool) (string, error) {
		if !required && strings.TrimSpace(ref) == "" && strings.TrimSpace(selector) == "" {
			return "", nil
		}
		return session.ResolveSelector(ref, selector)
	}

	switch action {
	case "status":
		tabs, statusErr := session.Tabs(ctx)
		if tabs == nil {
			tabs = []libbrowser.TabInfo{}
		}
		info := session.Info()
		out := map[string]any{"action": action, "session": info, "tabs": tabs}
		if statusErr != nil {
			info.Attached = false
			out["session"] = info
			out["cdp_error"] = statusErr.Error()
			out["hint"] = "La conexión CDP no respondió. Cierra y vuelve a iniciar la sesión si el navegador fue cerrado manualmente."
		}
		return jsonOutput(out)
	case "close":
		if err := manager.Close(str(args, "session_id")); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action})
	case "navigate":
		if err := session.Navigate(ctx, str(args, "url")); err != nil {
			return "", err
		}
		return browserActionSnapshot(ctx, session, action, args)
	case "back":
		if err := session.Back(ctx); err != nil {
			return "", err
		}
		return browserActionSnapshot(ctx, session, action, args)
	case "forward":
		if err := session.Forward(ctx); err != nil {
			return "", err
		}
		return browserActionSnapshot(ctx, session, action, args)
	case "reload":
		if err := session.Reload(ctx); err != nil {
			return "", err
		}
		return browserActionSnapshot(ctx, session, action, args)
	case "snapshot":
		snapshot, err := session.Snapshot(ctx, boolArgOr(args, "delta", false), intArgOr(args, "max_text", 8000), intArgOr(args, "max_elements", 120))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "snapshot": snapshot})
	case "text":
		sel, err := resolveSelector(false)
		if err != nil {
			return "", err
		}
		text, err := session.Text(ctx, sel, intArgOr(args, "max_bytes", 12000))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "text": text})
	case "html":
		sel, err := resolveSelector(false)
		if err != nil {
			return "", err
		}
		html, err := session.HTML(ctx, sel, intArgOr(args, "max_bytes", 30000))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "html": html})
	case "click":
		sel, err := resolveSelector(true)
		if err != nil {
			return "", err
		}
		if err := session.Click(ctx, sel); err != nil {
			return "", err
		}
		return browserActionSnapshot(ctx, session, action, args)
	case "type", "fill":
		sel, err := resolveSelector(true)
		if err != nil {
			return "", err
		}
		appendValue := action == "type" || boolArgOr(args, "append", false)
		if err := session.SetValue(ctx, sel, str(args, "value"), appendValue); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "ref": ref, "value_length": len(str(args, "value"))})
	case "fill_secret":
		sel, err := resolveSelector(true)
		if err != nil {
			return "", err
		}
		if env.RequestSecret == nil {
			return "", errors.New("entrada secreta local no disponible")
		}
		label := strings.TrimSpace(str(args, "secret_label"))
		message := "Escribe el secreto que debe introducirse en el formulario del navegador. El valor no se enviará al modelo, no aparecerá en los argumentos y no se guardará en el historial."
		if label != "" {
			message = "Escribe la contraseña o secreto para " + label + ". El valor no se enviará al modelo, no aparecerá en los argumentos y no se guardará en el historial."
		}
		secret, err := env.RequestSecret(ctx, interaction.SecretBrowserPassword, "Contraseña o secreto del sitio web", message, false, 1)
		if err != nil {
			return "", err
		}
		if err := session.SetValue(ctx, sel, secret, false); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "ref": ref, "secret_entered": true})
	case "select":
		sel, err := resolveSelector(true)
		if err != nil {
			return "", err
		}
		if err := session.SelectValue(ctx, sel, str(args, "value")); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action})
	case "key":
		if err := session.Key(ctx, str(args, "key")); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action})
	case "wait":
		sel, err := resolveSelector(false)
		if err != nil {
			return "", err
		}
		duration := time.Duration(intArgOr(args, "timeout_ms", 500)) * time.Millisecond
		if err := session.Wait(ctx, sel, duration); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action})
	case "evaluate":
		result, err := session.Evaluate(ctx, str(args, "expression"), intArgOr(args, "max_bytes", 16000))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "result": result})
	case "screenshot":
		sel, err := resolveSelector(false)
		if err != nil {
			return "", err
		}
		path, err := resolve(env.Root, str(args, "path"))
		if err != nil {
			return "", err
		}
		count, err := session.Screenshot(ctx, sel, path, boolArgOr(args, "full_page", true), intArgOr(args, "quality", 90))
		if err != nil {
			return "", err
		}
		rel := path
		if value, relErr := filepath.Rel(env.Root, path); relErr == nil && !strings.HasPrefix(value, "..") {
			rel = filepath.ToSlash(value)
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "path": rel, "bytes": count})
	case "console":
		values, err := session.Console(intArgOr(args, "limit", 100), boolArgOr(args, "clear", false))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "events": values, "count": len(values)})
	case "network":
		values, err := session.Network(intArgOr(args, "limit", 150), boolArgOr(args, "errors_only", false), boolArgOr(args, "clear", false))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "events": values, "count": len(values)})
	case "response_body":
		body, truncated, err := session.ResponseBody(ctx, str(args, "request_id"), intArgOr(args, "max_bytes", 30000))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "body": body, "truncated": truncated})
	case "import_cookies":
		path := resolveBrowserImportPath(str(args, "cookie_path"), env.Root)
		report, err := session.ImportCookies(ctx, path, str(args, "url"))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "cookies": report})
	case "tabs":
		values, err := session.Tabs(ctx)
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "tabs": values, "count": len(values)})
	case "new_tab":
		value, err := session.NewTab(ctx, str(args, "url"))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "tab": value})
	case "switch_tab":
		value, err := session.SwitchTab(ctx, str(args, "tab_id"))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action, "tab": value})
	case "close_tab":
		if err := session.CloseTab(ctx, str(args, "tab_id")); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": action})
	case "scripts":
		values, err := session.Scripts(ctx, intArgOr(args, "limit", 200), boolArgOr(args, "verify", true))
		if err != nil {
			return "", err
		}
		verified := 0
		for _, value := range values {
			if value.MappingVerified {
				verified++
			}
		}
		return jsonOutput(map[string]any{"action": action, "scripts": values, "count": len(values), "verified_count": verified})
	case "search_source":
		values, truncated, err := session.SearchSource(ctx, str(args, "script_id"), str(args, "query"), boolArgOr(args, "case_sensitive", false), intArgOr(args, "limit", 30))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "matches": values, "count": len(values), "truncated": truncated})
	case "performance":
		values, err := session.Performance(ctx)
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": action, "metrics": values})
	default:
		return "", fmt.Errorf("acción browser desconocida: %s", action)
	}
}

func browserActionSnapshot(ctx context.Context, session *libbrowser.Session, action string, args map[string]any) (string, error) {
	// A small post-action snapshot saves a follow-up tool call while still keeping
	// the payload bounded. Models can ask for a larger snapshot only when needed.
	_ = session.Wait(ctx, "", 250*time.Millisecond)
	snapshot, err := session.Snapshot(ctx, true, intArgOr(args, "max_text", 4000), intArgOr(args, "max_elements", 80))
	if err != nil {
		return jsonOutput(map[string]any{"ok": true, "action": action, "snapshot_error": err.Error()})
	}
	return jsonOutput(map[string]any{"ok": true, "action": action, "snapshot": snapshot})
}

func browserRecommendation(candidates []libbrowser.Candidate) map[string]any {
	if len(candidates) == 0 {
		return map[string]any{
			"available": false,
			"message":   "Instala Chrome, Chromium, Edge, Brave o Chrome for Testing, o proporciona un remote_url CDP.",
		}
	}
	best := candidates[0]
	return map[string]any{
		"available": true, "candidate_id": best.ID, "name": best.Name,
		"executable": best.Executable, "remote_url": best.RemoteURL, "safe_to_attach": best.SafeToAttach,
		"reason": best.Reason,
	}
}

func resolveBrowserImportPath(value, projectRoot string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			if value == "~" {
				return home
			}
			return filepath.Join(home, value[2:])
		}
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(projectRoot, value))
}
