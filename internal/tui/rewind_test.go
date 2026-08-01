package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/rewind"
	"github.com/lilith/li/internal/session"
)

func TestRewindAndForkCommandsAreRegisteredSeparately(t *testing.T) {
	rewindCommand := FindCommand("/rewind")
	if rewindCommand == nil || rewindCommand.Run == nil {
		t.Fatal("/rewind is not registered")
	}
	forkCommand := FindCommand("fork")
	if forkCommand == nil || forkCommand.Name != "fork" || forkCommand.Run == nil {
		t.Fatalf("/fork is not registered as a session fork: %+v", forkCommand)
	}
	subtask := FindCommand("subtask")
	if subtask == nil {
		t.Fatal("/subtask disappeared")
	}
	for _, alias := range subtask.Aliases {
		if alias == "fork" {
			t.Fatal("/fork must no longer alias the subagent command")
		}
	}
}

func TestRewindCommandRejectsActiveTurn(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.activeTurnID = 42
	command := FindCommand("rewind")
	if command == nil {
		t.Fatal("/rewind is not registered")
	}
	if cmd := command.Run(ctx, &model, ""); cmd != nil {
		t.Fatal("/rewind must not open while a turn is active")
	}
	if len(model.messages) == 0 || !strings.Contains(model.messages[len(model.messages)-1].Content, "sólo puede ejecutarse") {
		t.Fatalf("active-turn rejection was not shown: %+v", model.messages)
	}
}

func TestRewindCommandRejectsDirectCommandAndBackgroundAgent(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	command := FindCommand("rewind")
	if command == nil {
		t.Fatal("/rewind is not registered")
	}

	direct := NewChat(ctx)
	direct.beginRewindExternalOperation()
	if cmd := command.Run(ctx, &direct, ""); cmd != nil {
		t.Fatal("/rewind must not open while a direct command is running")
	}
	direct.endRewindExternalOperation()

	background := NewChat(ctx)
	background.agentPanels = map[string]*AgentPanel{"task-1": {TaskID: "task-1", Background: true, Status: "running"}}
	if cmd := command.Run(ctx, &background, ""); cmd != nil {
		t.Fatal("/rewind must not open while a background agent is running")
	}
}

func TestApplyRewindRestoresConversationAndPromptWithoutChangingTimelineID(t *testing.T) {
	project := t.TempDir()
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.project = project
	model.sess = session.New(project)
	currentID := model.sess.ID
	model.history = []openai.Message{{Role: "user", Content: "actual"}, {Role: "assistant", Content: "respuesta actual"}}
	model.messages = []ChatMessage{{Kind: MsgUser, Content: "actual"}, {Kind: MsgAssistant, Content: "respuesta actual"}}

	checkpoint := session.New(project)
	checkpoint.Messages = []openai.Message{{Role: "user", Content: "anterior"}, {Role: "assistant", Content: "respuesta anterior"}}
	checkpoint.Transcript = []session.TranscriptEntry{{Kind: "user", Content: "anterior"}, {Kind: "assistant", Content: "respuesta anterior"}}
	point := &rewind.Point{Meta: rewind.Meta{ID: "p1", SessionID: currentID, ProjectPath: project, Prompt: "actual", Kind: "turn", CreatedAt: time.Now()}, Conversation: checkpoint}

	model.applyRewindPoint(point, rewindConversation)
	if model.sess == nil || model.sess.ID != currentID {
		t.Fatalf("rewind must keep the active timeline ID: %+v", model.sess)
	}
	if model.project != filepath.Clean(project) || len(model.history) != 2 || model.history[0].Content != "anterior" {
		t.Fatalf("conversation was not restored: project=%s history=%+v", model.project, model.history)
	}
	if got := model.textarea.Value(); got != "actual" {
		t.Fatalf("selected user prompt was not returned to the editor: %q", got)
	}
	if len(model.messages) == 0 || !strings.Contains(model.messages[len(model.messages)-1].Content, "Rewind completado") {
		t.Fatalf("rewind confirmation missing: %+v", model.messages)
	}
}

func TestSafetyRewindDoesNotPopulateEditor(t *testing.T) {
	project := t.TempDir()
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.project = project
	model.sess = session.New(project)
	checkpoint := session.New(project)
	checkpoint.Messages = []openai.Message{{Role: "user", Content: "estado seguro"}}
	checkpoint.Transcript = []session.TranscriptEntry{{Kind: "user", Content: "estado seguro"}}
	point := &rewind.Point{Meta: rewind.Meta{ID: "safe", SessionID: model.sess.ID, ProjectPath: project, Prompt: "Estado antes de rewind", Kind: "safety"}, Conversation: checkpoint}

	model.textarea.SetValue("no conservar")
	model.applyRewindPoint(point, rewindConversation)
	if got := model.textarea.Value(); got != "" {
		t.Fatalf("safety point must not inject its internal label into editor: %q", got)
	}
}

func TestWorkspaceCaptureFailureIsAttemptedOnlyOnce(t *testing.T) {
	cfg := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-created")
	ctx := &AppContext{ConfigDir: cfg, Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.project = missing
	model.sess = session.New(missing)
	model.sess.Messages = []openai.Message{{Role: "user", Content: "base"}}
	model.beginRewindPoint("mutar")
	first := model.ensureActiveRewindWorkspace()
	if first == nil {
		t.Fatal("snapshot of a missing project should fail")
	}
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missing, "new.txt"), []byte("now exists"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := model.ensureActiveRewindWorkspace()
	if second == nil || second.Error() != first.Error() {
		t.Fatalf("failed snapshot should not be retried before every tool: first=%v second=%v", first, second)
	}
}

func TestForkRejectsEmptyConversation(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	if cmd := model.runForkSessionCommand(""); cmd != nil {
		t.Fatal("empty conversation must not start a fork command")
	}
	if len(model.messages) == 0 || !strings.Contains(model.messages[len(model.messages)-1].Content, "al menos un mensaje") {
		t.Fatalf("empty fork error was not shown: %+v", model.messages)
	}
}

func TestRewindToolMutationPolicy(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	if !model.rewindToolMayMutate("create_file") || !model.rewindToolMayMutate("run_terminal_command") || !model.rewindToolMayMutate("Agent") {
		t.Fatal("mutating built-in tools must trigger a workspace checkpoint")
	}
	if model.rewindToolMayMutate("read_files") || model.rewindToolMayMutate("search_files") {
		t.Fatal("read-only built-in tools must not scan the workspace")
	}
}

func TestForkCommandCreatesIndependentWorkspaceAndSession(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "main.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.project = project
	model.sess = session.New(project)
	model.history = []openai.Message{{Role: "user", Content: "crea una alternativa"}}
	model.messages = []ChatMessage{{Kind: MsgUser, Content: "crea una alternativa"}}

	cmd := model.runForkSessionCommand("Alternativa")
	if cmd == nil {
		t.Fatal("/fork did not open the destination selector")
	}
	raw := cmd()
	switchMsg, ok := raw.(switchScreenMsg)
	if !ok {
		t.Fatalf("unexpected /fork command result type: %T", raw)
	}
	selector, ok := switchMsg.next.(*ForkDestinationModel)
	if !ok || selector.title != "Alternativa" {
		t.Fatalf("/fork did not preserve the title in its selector: %#v", switchMsg.next)
	}

	destination := filepath.Join(t.TempDir(), "fork")
	cmd = model.startForkSessionAt("Alternativa", destination)
	if cmd == nil {
		t.Fatal("fork creation did not create a command")
	}
	raw = cmd()
	result, ok := raw.(forkSessionResultMsg)
	if !ok {
		t.Fatalf("unexpected fork result type: %T", raw)
	}
	if result.err != nil {
		t.Fatalf("fork failed: %v", result.err)
	}
	if result.sess == nil || result.sess.ID == model.sess.ID || result.sess.ProjectPath == project {
		t.Fatalf("fork session is not independent: %+v", result.sess)
	}
	if result.sess.Title != "Alternativa" || result.sess.ForkedFrom == nil || result.sess.ForkedFrom.SessionID != model.sess.ID {
		t.Fatalf("fork provenance/title missing: %+v", result.sess)
	}
	forkFile := filepath.Join(result.project, "main.txt")
	data, err := os.ReadFile(forkFile)
	if err != nil || string(data) != "original\n" {
		t.Fatalf("fork workspace missing original file: data=%q err=%v", data, err)
	}
	if err := os.WriteFile(forkFile, []byte("fork\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(project, "main.txt"))
	if err != nil || string(original) != "original\n" {
		t.Fatalf("fork changed source workspace: data=%q err=%v", original, err)
	}
}

func TestRewindCodeRestoreCreatesReversibleSafetyPoint(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "state.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	model := NewChat(ctx)
	model.project = project
	model.sess = session.New(project)
	model.history = []openai.Message{{Role: "user", Content: "cambia el archivo"}}
	model.messages = []ChatMessage{{Kind: MsgUser, Content: "cambia el archivo"}}

	conversation := model.snapshotSessionForRewind()
	meta, err := model.rewindStore.Create(project, model.sess.ID, "cambia el archivo", conversation)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = model.rewindStore.CaptureWorkspace(project, model.sess.ID, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	screen := &RewindModel{ctx: ctx, chat: &model, selected: meta}
	cmd := screen.startRestore(rewindCode)
	if cmd == nil {
		t.Fatal("rewind did not start")
	}
	raw := cmd()
	result, ok := raw.(rewindOperationResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("rewind failed: type=%T result=%+v", raw, result)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "before\n" {
		t.Fatalf("code was not restored: data=%q err=%v", got, err)
	}
	points, err := model.rewindStore.List(project, model.sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundSafety := false
	for _, point := range points {
		if point.Kind == "safety" && point.Prompt == "Estado antes de rewind" && point.HasCode {
			foundSafety = true
			break
		}
	}
	if !foundSafety {
		t.Fatalf("rewind did not create a reversible safety point: %+v", points)
	}
}
