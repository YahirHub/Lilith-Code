package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/debugger"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestRunInitialKeepsPersistentContextAliveAfterSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := func(runCtx context.Context, _ ...chromedp.Action) error {
		if runCtx != ctx {
			t.Fatal("runInitial creó o sustituyó el contexto persistente")
		}
		return nil
	}
	if err := runInitialWith(ctx, cancel, time.Second, runner); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("el contexto persistente quedó cancelado después del arranque: %v", err)
	}
}

func TestRunInitialCancelsPersistentContextOnTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := func(runCtx context.Context, _ ...chromedp.Action) error {
		<-runCtx.Done()
		return runCtx.Err()
	}
	err := runInitialWith(ctx, cancel, 5*time.Millisecond, runner)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("se esperaba deadline exceeded, se obtuvo %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("el contexto persistente no fue cancelado tras el timeout: %v", ctx.Err())
	}
}

func TestOperationContextCancellationDoesNotClosePersistentContext(t *testing.T) {
	persistent, stopPersistent := context.WithCancel(context.Background())
	defer stopPersistent()
	request, stopRequest := context.WithCancel(context.Background())
	opCtx, stopOperation := operationContext(request, persistent, time.Second)
	defer stopOperation()

	stopRequest()
	select {
	case <-opCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("la cancelación de la solicitud no alcanzó a la operación CDP")
	}
	if err := persistent.Err(); err != nil {
		t.Fatalf("cancelar una operación cerró el contexto persistente: %v", err)
	}
}

func TestEnableDebuggerImplementsAction(t *testing.T) {
	var action chromedp.Action = enableDebugger()
	if action == nil {
		t.Fatal("enableDebugger devolvió una acción nula")
	}
}

func TestExecutionContextsClearedDropsDocumentBoundState(t *testing.T) {
	tab := &Tab{
		scripts:      map[string]ScriptInfo{"90": {ID: "90", URL: "old.js"}},
		refs:         map[string]string{"e1": "#old"},
		selectorRefs: map[string]string{"#old": "e1"},
		nextRef:      8,
		lastElements: map[string]Element{"#old": {Ref: "e1"}},
		lastSnapshot: &Snapshot{Title: "Old"},
		lastTitle:    "Old",
		lastURL:      "https://example.test/old",
	}

	tab.recordEvent(&cdpruntime.EventExecutionContextsCleared{})

	if len(tab.scripts) != 0 || len(tab.refs) != 0 || len(tab.selectorRefs) != 0 || len(tab.lastElements) != 0 {
		t.Fatalf("el estado ligado al documento no fue limpiado: scripts=%v refs=%v selectors=%v elements=%v", tab.scripts, tab.refs, tab.selectorRefs, tab.lastElements)
	}
	if tab.nextRef != 1 || tab.lastSnapshot != nil || tab.lastTitle != "" || tab.lastURL != "" {
		t.Fatalf("el estado derivado del documento quedó activo: nextRef=%d snapshot=%v title=%q url=%q", tab.nextRef, tab.lastSnapshot, tab.lastTitle, tab.lastURL)
	}
	if tab.documentGeneration != 1 {
		t.Fatalf("la generación del documento no avanzó: %d", tab.documentGeneration)
	}

	tab.recordEvent(&debugger.EventScriptParsed{ScriptID: cdpruntime.ScriptID("91"), URL: "new.js"})
	if len(tab.scripts) != 1 || tab.scripts["91"].URL != "new.js" || tab.scripts["91"].DocumentGeneration != 1 {
		t.Fatalf("el inventario nuevo no fue reconstruido después de navegar: %#v", tab.scripts)
	}
	if _, stale := tab.scripts["90"]; stale {
		t.Fatal("el script del documento anterior reapareció en el inventario")
	}
}

func TestParseScriptContextAuxData(t *testing.T) {
	contextType, frameID, defaultContext := parseScriptContextAuxData([]byte(`{"isDefault":true,"type":"default","frameId":"FRAME-1"}`))
	if contextType != "default" || frameID != "FRAME-1" || !defaultContext {
		t.Fatalf("metadatos de contexto inesperados: type=%q frame=%q default=%v", contextType, frameID, defaultContext)
	}
}

func TestScriptContentHashMatchesV8SHA256(t *testing.T) {
	const want = "b3de078634b27f3dfa5a14464e9bd81fa877768ca37bd8ad986d0c465798abf1"
	if got := scriptContentHash("function foo13(){}", nil); got != want {
		t.Fatalf("hash SHA-256 incompatible con V8: got=%q want=%q", got, want)
	}
}

func TestReconcileScriptMetadataKeepsMatchingURL(t *testing.T) {
	source := "window.appShellReady = true;"
	hash := scriptContentHash(source, nil)
	reported := ScriptInfo{ID: "7", URL: "https://example.test/app-shell.js", Hash: hash, MappingSource: "debugger_event"}

	got := reconcileScriptMetadata([]ScriptInfo{reported}, []ScriptInfo{reported}, map[string]string{"7": hash}, nil)
	if len(got) != 1 {
		t.Fatalf("resultado inesperado: %#v", got)
	}
	if got[0].URL != reported.URL || got[0].ReportedURL != "" || !got[0].MappingVerified {
		t.Fatalf("el mapeo válido fue alterado: %#v", got[0])
	}
	if got[0].MappingSource != "content_hash" {
		t.Fatalf("origen de mapeo inesperado: %#v", got[0])
	}
}

func TestReconcileScriptMetadataByContentHash(t *testing.T) {
	themeSource := "const themeMedia = window.matchMedia?.('(prefers-color-scheme: dark)');"
	appSource := "window.appShellReady = true;"
	themeHash := scriptContentHash(themeSource, nil)
	appHash := scriptContentHash(appSource, nil)

	all := []ScriptInfo{
		{ID: "6", URL: "https://example.test/theme-init.js", Hash: themeHash, MappingSource: "debugger_event"},
		{ID: "7", URL: "https://example.test/app-shell.js", Hash: appHash, MappingSource: "debugger_event"},
	}
	actual := map[string]string{
		"6": appHash,
		"7": themeHash,
	}

	got := reconcileScriptMetadata(all, all, actual, nil)
	byID := make(map[string]ScriptInfo, len(got))
	for _, script := range got {
		byID[script.ID] = script
	}
	if byID["6"].URL != "https://example.test/app-shell.js" || byID["6"].ReportedURL != "https://example.test/theme-init.js" {
		t.Fatalf("el id 6 no fue reconciliado: %#v", byID["6"])
	}
	if byID["7"].URL != "https://example.test/theme-init.js" || byID["7"].ReportedURL != "https://example.test/app-shell.js" {
		t.Fatalf("el id 7 no fue reconciliado: %#v", byID["7"])
	}
	if !byID["6"].MappingVerified || !byID["7"].MappingVerified {
		t.Fatalf("los mapeos reconciliados deben quedar verificados: %#v", byID)
	}
}

func TestReconcileScriptMetadataHidesUnresolvedURL(t *testing.T) {
	reported := ScriptInfo{ID: "7", URL: "https://example.test/app-shell.js", Hash: scriptContentHash("old", nil)}
	actualHash := scriptContentHash("new source without metadata", nil)
	got := reconcileScriptMetadata([]ScriptInfo{reported}, []ScriptInfo{reported}, map[string]string{"7": actualHash}, nil)
	if len(got) != 1 {
		t.Fatalf("resultado inesperado: %#v", got)
	}
	if got[0].URL != "" || got[0].ReportedURL != reported.URL || got[0].MappingVerified {
		t.Fatalf("se expuso una URL no verificable: %#v", got[0])
	}
	if got[0].MappingSource != "content_hash_unresolved" {
		t.Fatalf("origen de mapeo inesperado: %#v", got[0])
	}
}

func TestSearchSourceRejectsScriptOutsideCurrentDocument(t *testing.T) {
	tab := &Tab{ctx: context.Background(), scripts: map[string]ScriptInfo{}}
	session := &Session{tabs: map[string]*Tab{"tab": tab}, currentTab: "tab"}

	_, _, err := session.SearchSource(context.Background(), "90", "geocode", false, 30)
	if err == nil || !strings.Contains(err.Error(), "documento actual") {
		t.Fatalf("se esperaba un error de script caducado y accionable, se obtuvo %v", err)
	}
}

func TestFormatSearchMatchesUsesOneBasedLinesAndLimit(t *testing.T) {
	matches, truncated := formatSearchMatches([]*debugger.SearchMatch{
		{LineNumber: 0, LineContent: "const marker = true;"},
		{LineNumber: 4, LineContent: "marker();"},
	}, 1)
	if !truncated {
		t.Fatal("el resultado debía indicar truncamiento")
	}
	if len(matches) != 1 || matches[0]["line"] != 1 || matches[0]["text"] != "const marker = true;" {
		t.Fatalf("resultado de búsqueda inesperado: %#v", matches)
	}
}

func TestInvalidScriptIDErrorDetection(t *testing.T) {
	for _, message := range []string{"No script for id: 90 (-32000)", "Cannot find script 90", "Script with given id was not found"} {
		if !isInvalidScriptIDError(errors.New(message)) {
			t.Fatalf("no se detectó el error de script inválido: %q", message)
		}
	}
	if isInvalidScriptIDError(errors.New("context canceled")) {
		t.Fatal("un error ajeno fue clasificado como script inválido")
	}
}

func TestStartDoesNotPassUnsupportedExecAllocatorFlags(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "fake-browser")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.WriteFile(executable, []byte("not a browser"), 0o755); err != nil {
		t.Fatal(err)
	}

	manager := &Manager{configDir: t.TempDir(), sessions: map[string]*Session{}}
	_, err := manager.Start(context.Background(), StartOptions{
		SessionID:   "allocator-flags",
		Executable:  executable,
		ProfileMode: ProfileTemporary,
	})
	if err == nil {
		t.Fatal("se esperaba un error al intentar ejecutar un archivo que no es navegador")
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid exec pool flag") {
		t.Fatalf("Chromedp recibió un valor de flag no compatible: %v", err)
	}
}

func TestValidateSessionID(t *testing.T) {
	for _, value := range []string{"jsecure", "panel-pruebas", "browser_01", "qa.local"} {
		if err := validateSessionID(value); err != nil {
			t.Fatalf("session_id válido %q fue rechazado: %v", value, err)
		}
	}
	for _, value := range []string{"con espacios", "ruta/otra", "comando;rm", strings.Repeat("a", 81)} {
		if err := validateSessionID(value); err == nil {
			t.Fatalf("session_id inválido %q fue aceptado", value)
		}
	}
}

func TestBrowserIntegrationStartAndSnapshot(t *testing.T) {
	if os.Getenv("LILITH_BROWSER_INTEGRATION") != "1" {
		t.Skip("define LILITH_BROWSER_INTEGRATION=1 para ejecutar la prueba con un navegador real")
	}
	executable := strings.TrimSpace(os.Getenv("LILITH_BROWSER_EXECUTABLE"))
	if executable == "" {
		candidates, err := Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range candidates {
			if candidate.Executable != "" {
				executable = candidate.Executable
				break
			}
		}
	}
	if executable == "" {
		t.Fatal("no se encontró un navegador para la prueba de integración")
	}

	manager := &Manager{configDir: t.TempDir(), sessions: map[string]*Session{}}
	info, err := manager.Start(context.Background(), StartOptions{
		SessionID:   "integration",
		Executable:  executable,
		Headless:    true,
		ProfileMode: ProfileTemporary,
		StartURL:    "data:text/html,<title>Lilith Browser Test</title><main><button id='ready'>Listo</button></main>",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.CloseAll()
	if info.ID != "integration" {
		t.Fatalf("session_id inesperado: %q", info.ID)
	}

	session, err := manager.Session(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Info().Attached {
		t.Fatal("la sesión CDP figura desconectada inmediatamente después de start")
	}
	if err := session.Navigate(context.Background(), "data:text/html,<title>Lilith Persistent CDP</title><script>window.lilithSearchMarker='search-source-ok'</script><main><button id='ready'>Listo</button></main>"); err != nil {
		t.Fatalf("navigate posterior a start falló: %v", err)
	}
	snapshot, err := session.Snapshot(context.Background(), false, 2000, 20)
	if err != nil {
		t.Fatalf("snapshot posterior a navigate falló: %v", err)
	}
	if snapshot.Title != "Lilith Persistent CDP" {
		t.Fatalf("título inesperado: %q", snapshot.Title)
	}
	if len(snapshot.Elements) == 0 || snapshot.Elements[0].Text != "Listo" {
		t.Fatalf("snapshot no contiene el botón esperado: %#v", snapshot.Elements)
	}
	scripts, err := session.Scripts(context.Background(), 0, true)
	if err != nil {
		t.Fatalf("scripts posterior a navigate falló: %v", err)
	}
	foundSource := false
	for _, script := range scripts {
		matches, _, searchErr := session.SearchSource(context.Background(), script.ID, "search-source-ok", true, 10)
		if searchErr == nil && len(matches) > 0 {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Fatalf("search_source no encontró el marcador del documento actual; scripts=%#v", scripts)
	}
	tabs, err := session.Tabs(context.Background())
	if err != nil {
		t.Fatalf("status/tabs posterior a snapshot falló: %v", err)
	}
	if len(tabs) == 0 {
		t.Fatal("status devolvió una lista de pestañas vacía")
	}
	screenshot := filepath.Join(t.TempDir(), "persistent.png")
	if _, err := session.Screenshot(context.Background(), "", screenshot, false, 0); err != nil {
		t.Fatalf("screenshot posterior a varias acciones falló: %v", err)
	}
	if info, err := os.Stat(screenshot); err != nil || info.Size() == 0 {
		t.Fatalf("screenshot no fue generado correctamente: info=%v err=%v", info, err)
	}
}
