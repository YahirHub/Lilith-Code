package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/tui/uikit"
)

func newForkDestinationTestModel(t *testing.T) (*ForkDestinationModel, *ChatModel, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme()), Width: 80, Height: 24}
	chat := NewChat(ctx)
	chat.project = project
	chat.sess = session.New(project)
	chat.history = []openai.Message{{Role: "user", Content: "crea una alternativa"}}
	chat.messages = []ChatMessage{{Kind: MsgUser, Content: "crea una alternativa"}}
	selector := NewForkDestinationScreen(ctx, chat, "Alternativa")
	return selector, chat, root
}

func forkEntryIndex(entries []forkBrowserEntry, kind forkBrowserEntryKind, path string) int {
	for i, entry := range entries {
		if entry.kind == kind && (path == "" || filepath.Clean(entry.path) == filepath.Clean(path)) {
			return i
		}
	}
	return -1
}

func clickForkEntry(t *testing.T, selector *ForkDestinationModel, index int) uikit.Cmd {
	t.Helper()
	rect, offset := selector.browserListGeometry()
	y := rect.y + index - offset
	if y < rect.y || y >= rect.y+rect.h {
		t.Fatalf("entry %d is not visible: rect=%+v offset=%d", index, rect, offset)
	}
	_, cmd := selector.Update(uikit.MouseMsg(uikit.MouseEvent{
		X:      rect.x + 1,
		Y:      y,
		Action: uikit.MouseActionPress,
		Button: uikit.MouseButtonLeft,
	}))
	return cmd
}

func TestForkDestinationSupportsKeyboardNavigationBackAndCreate(t *testing.T) {
	selector, _, root := newForkDestinationTestModel(t)
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	selector.reload()

	index := forkEntryIndex(selector.entries, forkEntryDirectory, empty)
	if index < 0 {
		t.Fatalf("empty directory was not listed: %+v", selector.entries)
	}
	selector.cursor = index
	_, _ = selector.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	if filepath.Clean(selector.currentDir) != filepath.Clean(empty) {
		t.Fatalf("Enter did not open the selected directory: %s", selector.currentDir)
	}
	_, _ = selector.Update(uikit.KeyMsg{Type: uikit.KeyBackspace})
	if filepath.Clean(selector.currentDir) != filepath.Clean(root) {
		t.Fatalf("Backspace did not return to the parent: %s", selector.currentDir)
	}

	_, _ = selector.Update(uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune{'n'}})
	if selector.stage != forkDestinationCreateFolder {
		t.Fatal("N did not open the create-folder form")
	}
	selector.folderName.SetValue("fork-dest")
	_, _ = selector.Update(uikit.KeyMsg{Type: uikit.KeyEnter})
	created := filepath.Join(root, "fork-dest")
	if selector.stage != forkDestinationBrowse || filepath.Clean(selector.currentDir) != filepath.Clean(created) {
		t.Fatalf("new folder was not created and opened: stage=%v path=%s", selector.stage, selector.currentDir)
	}
	if info, err := os.Stat(created); err != nil || !info.IsDir() {
		t.Fatalf("created folder missing: info=%v err=%v", info, err)
	}
}

func TestForkDestinationMouseOpensFoldersAndUsesCreateButton(t *testing.T) {
	selector, _, root := newForkDestinationTestModel(t)
	empty := filepath.Join(root, "mouse-empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	selector.reload()
	selector.cursor = 0
	_, _ = selector.Update(uikit.MouseMsg(uikit.MouseEvent{
		Action: uikit.MouseActionPress,
		Button: uikit.MouseButtonWheelDown,
	}))
	if selector.cursor != 1 {
		t.Fatalf("mouse wheel did not move the selection: %d", selector.cursor)
	}

	index := forkEntryIndex(selector.entries, forkEntryDirectory, empty)
	if index < 0 {
		t.Fatal("mouse target directory was not listed")
	}
	clickForkEntry(t, selector, index)
	if filepath.Clean(selector.currentDir) != filepath.Clean(empty) {
		t.Fatalf("mouse click did not open directory: %s", selector.currentDir)
	}

	parentIndex := forkEntryIndex(selector.entries, forkEntryParent, root)
	if parentIndex < 0 {
		t.Fatal("parent action was not listed")
	}
	clickForkEntry(t, selector, parentIndex)
	if filepath.Clean(selector.currentDir) != filepath.Clean(root) {
		t.Fatalf("mouse click did not navigate back: %s", selector.currentDir)
	}

	createIndex := forkEntryIndex(selector.entries, forkEntryCreateFolder, "")
	clickForkEntry(t, selector, createIndex)
	selector.folderName.SetValue("clicked-folder")
	_, hits := selector.createFolderLayout()
	var createHit settingsHit
	found := false
	for _, hit := range hits {
		if hit.id == "fork-folder-create" {
			createHit = hit
			found = true
			break
		}
	}
	if !found {
		t.Fatal("create-folder button has no mouse hitbox")
	}
	_, _ = selector.Update(uikit.MouseMsg(uikit.MouseEvent{
		X:      createHit.rect.x,
		Y:      createHit.rect.y,
		Action: uikit.MouseActionPress,
		Button: uikit.MouseButtonLeft,
	}))
	if filepath.Clean(selector.currentDir) != filepath.Clean(filepath.Join(root, "clicked-folder")) {
		t.Fatalf("mouse create button did not create/open folder: %s", selector.currentDir)
	}
}

func TestForkDestinationStartsOutsideGitWorkspaceRootAndCapturesMouse(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	project := filepath.Join(repo, "nested", "app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme()), Width: 80, Height: 24}
	chat := NewChat(ctx)
	chat.project = project
	selector := NewForkDestinationScreen(ctx, chat, "")
	if filepath.Clean(selector.sourceRoot) != filepath.Clean(repo) {
		t.Fatalf("selector source root=%s, want Git root %s", selector.sourceRoot, repo)
	}
	if filepath.Clean(selector.currentDir) != filepath.Clean(filepath.Dir(repo)) {
		t.Fatalf("selector started at %s, want outside repository %s", selector.currentDir, filepath.Dir(repo))
	}
	root := RootModel{ctx: ctx, chat: chat, current: selector}
	if !root.wantsMouseCapture() {
		t.Fatal("destination selector must enable mouse capture")
	}
}

func TestForkDestinationRequiresEmptyFolder(t *testing.T) {
	selector, _, _ := newForkDestinationTestModel(t)
	selector.currentDir = selector.chat.project
	selector.reload()
	_, cmd := selector.selectCurrentDirectory()
	if cmd != nil {
		t.Fatal("non-empty project directory must not start a fork")
	}
	if !strings.Contains(selector.err, "debe estar vacía") {
		t.Fatalf("missing empty-directory validation: %q", selector.err)
	}
}

func TestForkDestinationCreatesForkInSelectedExistingFolder(t *testing.T) {
	selector, _, root := newForkDestinationTestModel(t)
	destination := filepath.Join(root, "selected-empty")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	selector.currentDir = destination
	selector.reload()

	_, cmd := selector.selectCurrentDirectory()
	if cmd == nil || selector.stage != forkDestinationWorking {
		t.Fatal("empty selected folder did not start fork creation")
	}
	raw := cmd()
	result, ok := raw.(forkSessionResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("fork creation failed: type=%T result=%+v", raw, result)
	}
	data, err := os.ReadFile(filepath.Join(destination, "main.txt"))
	if err != nil || string(data) != "original\n" {
		t.Fatalf("selected folder did not receive fork workspace: data=%q err=%v", data, err)
	}
}

func TestForkFolderNameRejectsPortableInvalidNames(t *testing.T) {
	for _, name := range []string{"", "..", "bad/name", "bad:name", "CON", "file."} {
		if err := validateForkFolderName(name); err == nil {
			t.Fatalf("invalid portable folder name was accepted: %q", name)
		}
	}
	if err := validateForkFolderName("fork-alternativo"); err != nil {
		t.Fatalf("valid folder name was rejected: %v", err)
	}
}
