package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/debugger"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type rawSnapshot struct {
	Title    string       `json:"title"`
	URL      string       `json:"url"`
	Text     string       `json:"text"`
	Elements []rawElement `json:"elements"`
}

type rawElement struct {
	Selector    string `json:"selector"`
	Tag         string `json:"tag"`
	Role        string `json:"role"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Text        string `json:"text"`
	Placeholder string `json:"placeholder"`
	Href        string `json:"href"`
	Disabled    bool   `json:"disabled"`
}

func (s *Session) Snapshot(ctx context.Context, delta bool, maxText, maxElements int) (Snapshot, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return Snapshot{}, err
	}
	if maxText <= 0 {
		maxText = 8000
	}
	if maxText > 50000 {
		maxText = 50000
	}
	if maxElements <= 0 {
		maxElements = 120
	}
	if maxElements > 500 {
		maxElements = 500
	}
	js := snapshotScript(maxText, maxElements)
	var encoded string
	if err := runWithRequestTimeout(ctx, tab.ctx, 25*time.Second, chromedp.Evaluate(js, &encoded)); err != nil {
		return Snapshot{}, err
	}
	var raw rawSnapshot
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		return Snapshot{}, fmt.Errorf("decodificar snapshot del navegador: %w", err)
	}

	now := time.Now()
	tab.mu.Lock()
	current := make(map[string]Element, len(raw.Elements))
	elements := make([]Element, 0, len(raw.Elements))
	for _, item := range raw.Elements {
		selector := strings.TrimSpace(item.Selector)
		if selector == "" {
			continue
		}
		ref := tab.selectorRefs[selector]
		if ref == "" {
			ref = fmt.Sprintf("e%d", tab.nextRef)
			tab.nextRef++
			tab.selectorRefs[selector] = ref
		}
		tab.refs[ref] = selector
		element := Element{
			Ref: ref, Tag: item.Tag, Role: item.Role, Type: item.Type,
			Name: truncate(item.Name, 240), Text: truncate(item.Text, 320),
			Placeholder: truncate(item.Placeholder, 160), Href: truncate(item.Href, 500), Disabled: item.Disabled,
		}
		current[selector] = element
		elements = append(elements, element)
	}
	result := Snapshot{
		Title: truncate(raw.Title, 500), URL: raw.URL, Text: truncate(raw.Text, maxText),
		Elements: elements, Delta: false, GeneratedAt: now,
		Truncated: len(raw.Text) >= maxText || len(raw.Elements) >= maxElements,
	}
	if delta && tab.lastSnapshot != nil {
		result.Delta = true
		result.Elements = nil
		if tab.lastSnapshot.Title == result.Title {
			result.Title = ""
		}
		if tab.lastSnapshot.URL == result.URL {
			result.URL = ""
		}
		if tab.lastSnapshot.Text == result.Text {
			result.Text = ""
		}
		for selector, element := range current {
			previous, ok := tab.lastElements[selector]
			if !ok {
				result.Added = append(result.Added, element)
			} else if previous != element {
				result.Changed = append(result.Changed, element)
			}
		}
		for selector, previous := range tab.lastElements {
			if _, ok := current[selector]; !ok {
				result.Removed = append(result.Removed, previous.Ref)
			}
		}
		tab.lastSnapshot.Title = raw.Title
		tab.lastSnapshot.URL = raw.URL
		tab.lastSnapshot.Text = truncate(raw.Text, maxText)
		tab.lastSnapshot.GeneratedAt = now
	} else {
		copySnapshot := result
		tab.lastSnapshot = &copySnapshot
	}
	tab.lastTitle = raw.Title
	tab.lastURL = raw.URL
	tab.lastElements = current
	tab.lastActivity = now
	tab.mu.Unlock()
	s.Touch()
	return result, nil
}

func snapshotScript(maxText, maxElements int) string {
	return fmt.Sprintf(`(() => {
  const maxText = %d;
  const maxElements = %d;
  const clean = (v, n) => String(v || '').replace(/\s+/g, ' ').trim().slice(0, n);
  const visible = (el) => {
    const s = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    return s.visibility !== 'hidden' && s.display !== 'none' && Number(s.opacity || 1) > 0 && r.width > 0 && r.height > 0;
  };
  const esc = (v) => window.CSS && CSS.escape ? CSS.escape(v) : String(v).replace(/[^a-zA-Z0-9_-]/g, '\\$&');
  const selector = (el) => {
    if (el.id) return '#' + esc(el.id);
    for (const attr of ['data-testid','data-test','data-qa']) {
      const v = el.getAttribute(attr);
      if (v) return '[' + attr + '="' + String(v).replace(/"/g, '\\"') + '"]';
    }
    const name = el.getAttribute('name');
    if (name) {
      const candidate = el.tagName.toLowerCase() + '[name="' + String(name).replace(/"/g, '\\"') + '"]';
      if (document.querySelectorAll(candidate).length === 1) return candidate;
    }
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1 && node !== document.documentElement) {
      let part = node.tagName.toLowerCase();
      const parent = node.parentElement;
      if (parent) {
        const same = Array.from(parent.children).filter(x => x.tagName === node.tagName);
        if (same.length > 1) part += ':nth-of-type(' + (same.indexOf(node) + 1) + ')';
      }
      parts.unshift(part);
      if (parts.length >= 7) break;
      node = parent;
    }
    return parts.join(' > ');
  };
  const label = (el) => {
    const aria = el.getAttribute('aria-label');
    if (aria) return aria;
    if (el.labels && el.labels.length) return Array.from(el.labels).map(x => x.innerText).join(' ');
    return el.getAttribute('title') || el.getAttribute('name') || '';
  };
  const nodes = Array.from(document.querySelectorAll('a,button,input,textarea,select,summary,[role],[contenteditable="true"]'))
    .filter(visible).slice(0, maxElements);
  const elements = nodes.map(el => ({
    selector: selector(el),
    tag: el.tagName.toLowerCase(),
    role: el.getAttribute('role') || '',
    type: el.getAttribute('type') || '',
    name: clean(label(el), 240),
    text: clean((el.type === 'password' || /pass(word)?|secret|token|api.?key/i.test((el.name || '') + ' ' + (el.id || ''))) ? '' : (el.innerText || ''), 320),
    placeholder: clean(el.getAttribute('placeholder'), 160),
    href: el.href || '',
    disabled: !!el.disabled || el.getAttribute('aria-disabled') === 'true'
  }));
  return JSON.stringify({
    title: document.title || '',
    url: location.href,
    text: clean(document.body ? document.body.innerText : '', maxText),
    elements
  });
})()`, maxText, maxElements)
}

func (s *Session) ResolveSelector(ref, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		return selector, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("se requiere ref o selector")
	}
	tab, err := s.CurrentTab()
	if err != nil {
		return "", err
	}
	tab.mu.RLock()
	selector = tab.refs[ref]
	tab.mu.RUnlock()
	if selector == "" {
		return "", fmt.Errorf("referencia %s no encontrada o caducada; toma un snapshot nuevo", ref)
	}
	return selector, nil
}

func (s *Session) Back(ctx context.Context) error {
	return s.runCurrent(ctx, 20*time.Second, chromedp.Evaluate(`history.back()`, nil), chromedp.Sleep(500*time.Millisecond))
}

func (s *Session) Forward(ctx context.Context) error {
	return s.runCurrent(ctx, 20*time.Second, chromedp.Evaluate(`history.forward()`, nil), chromedp.Sleep(500*time.Millisecond))
}

func (s *Session) Reload(ctx context.Context) error {
	return s.runCurrent(ctx, 30*time.Second, chromedp.Reload())
}

func (s *Session) Click(ctx context.Context, selector string) error {
	return s.runCurrent(ctx, 25*time.Second, chromedp.ScrollIntoView(selector, chromedp.ByQuery), chromedp.Click(selector, chromedp.ByQuery))
}

func (s *Session) SetValue(ctx context.Context, selector, value string, appendValue bool) error {
	if appendValue {
		return s.runCurrent(ctx, 25*time.Second, chromedp.ScrollIntoView(selector, chromedp.ByQuery), chromedp.SendKeys(selector, value, chromedp.ByQuery))
	}
	return s.runCurrent(ctx, 25*time.Second, chromedp.ScrollIntoView(selector, chromedp.ByQuery), chromedp.SetValue(selector, value, chromedp.ByQuery))
}

func (s *Session) SelectValue(ctx context.Context, selector, value string) error {
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	js := fmt.Sprintf(`(() => { const el=document.querySelector(%s); if(!el) throw new Error('selector not found'); el.value=%s; el.dispatchEvent(new Event('input',{bubbles:true})); el.dispatchEvent(new Event('change',{bubbles:true})); return el.value; })()`, sel, val)
	return s.runCurrent(ctx, 25*time.Second, chromedp.Evaluate(js, nil))
}

func (s *Session) Key(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("key es obligatorio")
	}
	return s.runCurrent(ctx, 20*time.Second, chromedp.KeyEvent(key))
}

func (s *Session) Wait(ctx context.Context, selector string, duration time.Duration) error {
	if selector != "" {
		if duration <= 0 {
			duration = 20 * time.Second
		}
		tab, err := s.CurrentTab()
		if err != nil {
			return err
		}
		return runWithRequestTimeout(ctx, tab.ctx, duration, chromedp.WaitVisible(selector, chromedp.ByQuery))
	}
	if duration <= 0 {
		duration = 500 * time.Millisecond
	}
	return s.runCurrent(ctx, duration+time.Second, chromedp.Sleep(duration))
}

func (s *Session) Text(ctx context.Context, selector string, maxBytes int) (string, error) {
	if selector == "" {
		selector = "body"
	}
	var out string
	if err := s.runCurrent(ctx, 25*time.Second, chromedp.Text(selector, &out, chromedp.ByQuery)); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = 12000
	}
	return truncate(out, maxBytes), nil
}

func (s *Session) HTML(ctx context.Context, selector string, maxBytes int) (string, error) {
	if selector == "" {
		selector = "html"
	}
	var out string
	if err := s.runCurrent(ctx, 25*time.Second, chromedp.OuterHTML(selector, &out, chromedp.ByQuery)); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = 30000
	}
	return truncate(out, maxBytes), nil
}

func (s *Session) Evaluate(ctx context.Context, expression string, maxBytes int) (string, error) {
	if strings.TrimSpace(expression) == "" {
		return "", errors.New("expression es obligatoria")
	}
	var out any
	if err := s.runCurrent(ctx, 25*time.Second, chromedp.Evaluate(expression, &out)); err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = 16000
	}
	return truncate(string(data), maxBytes), nil
}

func (s *Session) Screenshot(ctx context.Context, selector, path string, fullPage bool, quality int) (int, error) {
	if strings.TrimSpace(path) == "" {
		return 0, errors.New("path es obligatorio")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	var data []byte
	var action chromedp.Action
	if selector != "" {
		action = chromedp.Screenshot(selector, &data, chromedp.ByQuery)
	} else if fullPage {
		if quality <= 0 || quality > 100 {
			quality = 90
		}
		action = chromedp.FullScreenshot(&data, quality)
	} else {
		action = chromedp.CaptureScreenshot(&data)
	}
	if err := s.runCurrent(ctx, 40*time.Second, action); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (s *Session) Console(limit int, clear bool) ([]ConsoleEvent, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return nil, err
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if limit <= 0 || limit > len(tab.console) {
		limit = len(tab.console)
	}
	out := append([]ConsoleEvent(nil), tab.console[len(tab.console)-limit:]...)
	if clear {
		tab.console = nil
	}
	return out, nil
}

func (s *Session) Network(limit int, errorsOnly, clear bool) ([]NetworkEvent, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return nil, err
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	var values []NetworkEvent
	for _, event := range tab.network {
		if errorsOnly && !event.Failed && event.Status < 400 {
			continue
		}
		values = append(values, event)
	}
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := append([]NetworkEvent(nil), values[len(values)-limit:]...)
	if clear {
		tab.network = nil
	}
	return out, nil
}

func (s *Session) ResponseBody(ctx context.Context, requestID string, maxBytes int) (string, bool, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return "", false, err
	}
	var body []byte
	if err := runTargetCommand(ctx, tab.ctx, 15*time.Second, func(targetCtx context.Context) error {
		var bodyErr error
		body, bodyErr = network.GetResponseBody(network.RequestID(requestID)).Do(targetCtx)
		return bodyErr
	}); err != nil {
		return "", false, err
	}
	if maxBytes <= 0 {
		maxBytes = 30000
	}
	text := string(body)
	truncated := len(text) > maxBytes
	return truncate(text, maxBytes), truncated, nil
}

func (s *Session) Scripts(limit int) ([]ScriptInfo, error) {
	tab, err := s.CurrentTab()
	if err != nil {
		return nil, err
	}
	tab.mu.RLock()
	out := make([]ScriptInfo, 0, len(tab.scripts))
	for _, script := range tab.scripts {
		out = append(out, script)
	}
	tab.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Session) SearchSource(ctx context.Context, scriptID, query string, caseSensitive bool, maxMatches int) ([]map[string]any, bool, error) {
	scriptID = strings.TrimSpace(scriptID)
	if scriptID == "" || query == "" {
		return nil, false, errors.New("script_id y query son obligatorios")
	}
	tab, err := s.CurrentTab()
	if err != nil {
		return nil, false, err
	}
	tab.mu.RLock()
	_, known := tab.scripts[scriptID]
	tab.mu.RUnlock()
	if !known {
		return nil, false, fmt.Errorf("script_id %q no pertenece al documento actual; ejecuta scripts nuevamente y usa uno de los ids devueltos", scriptID)
	}

	var rawMatches []*debugger.SearchMatch
	if err := runTargetCommand(ctx, tab.ctx, 15*time.Second, func(targetCtx context.Context) error {
		var searchErr error
		rawMatches, searchErr = debugger.SearchInContent(cdpruntime.ScriptID(scriptID), query).
			WithCaseSensitive(caseSensitive).
			Do(targetCtx)
		return searchErr
	}); err != nil {
		if isInvalidScriptIDError(err) {
			tab.mu.Lock()
			delete(tab.scripts, scriptID)
			tab.mu.Unlock()
			return nil, false, fmt.Errorf("script_id %q caducó durante una navegación; ejecuta scripts nuevamente y usa un id del documento actual", scriptID)
		}
		return nil, false, err
	}
	matches, truncated := formatSearchMatches(rawMatches, maxMatches)
	return matches, truncated, nil
}

func formatSearchMatches(raw []*debugger.SearchMatch, maxMatches int) ([]map[string]any, bool) {
	if maxMatches <= 0 {
		maxMatches = 30
	}
	truncatedResult := len(raw) > maxMatches
	if truncatedResult {
		raw = raw[:maxMatches]
	}
	matches := make([]map[string]any, 0, len(raw))
	for _, match := range raw {
		if match == nil {
			continue
		}
		matches = append(matches, map[string]any{
			"line": int(match.LineNumber) + 1,
			"text": truncate(match.LineContent, 500),
		})
	}
	return matches, truncatedResult
}

func isInvalidScriptIDError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no script for id") ||
		strings.Contains(message, "cannot find script") ||
		strings.Contains(message, "script with given id")
}

func (s *Session) Performance(ctx context.Context) (map[string]any, error) {
	js := `(() => {
 const nav=performance.getEntriesByType('navigation')[0];
 const mem=performance.memory || {};
 return {
   url: location.href,
   navigation: nav ? {
     duration: nav.duration, domContentLoaded: nav.domContentLoadedEventEnd,
     loadEvent: nav.loadEventEnd, responseStart: nav.responseStart,
     transferSize: nav.transferSize, encodedBodySize: nav.encodedBodySize,
     decodedBodySize: nav.decodedBodySize
   } : null,
   resources: performance.getEntriesByType('resource').length,
   memory: {usedJSHeapSize: mem.usedJSHeapSize || 0, totalJSHeapSize: mem.totalJSHeapSize || 0}
 };
})()`
	var out map[string]any
	if err := s.runCurrent(ctx, 20*time.Second, chromedp.Evaluate(js, &out)); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Session) runCurrent(request context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	tab, err := s.CurrentTab()
	if err != nil {
		return err
	}
	if err := runWithRequestTimeout(request, tab.ctx, timeout, actions...); err != nil {
		return err
	}
	s.Touch()
	return nil
}

func runWithTimeout(parent context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	return runWithRequestTimeout(nil, parent, timeout, actions...)
}

func runWithRequestTimeout(request, persistent context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	ctx, cancel := operationContext(request, persistent, timeout)
	defer cancel()
	return chromedp.Run(ctx, actions...)
}
