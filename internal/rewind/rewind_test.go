package rewind

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
)

func TestStoreCreateListLoadAndDelete(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	store := NewStore(cfg)
	conversation := session.New(project)
	conversation.Messages = []openai.Message{{Role: "user", Content: "mensaje anterior"}}

	prompt := "  corrige el parser\ncon sangría  "
	meta, err := store.Create(project, conversation.ID, prompt, conversation)
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if meta.Kind != "turn" || meta.Prompt != prompt || meta.HasCode {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	items, err := store.List(project, conversation.ID)
	if err != nil || len(items) != 1 || items[0].ID != meta.ID {
		t.Fatalf("list checkpoints: %+v err=%v", items, err)
	}
	loaded, err := store.Load(project, conversation.ID, meta.ID)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if loaded.Conversation == nil || len(loaded.Conversation.Messages) != 1 || loaded.Conversation.Messages[0].Content != "mensaje anterior" {
		t.Fatalf("conversation was not persisted: %+v", loaded.Conversation)
	}
	if loaded.Meta.Prompt != prompt {
		t.Fatalf("checkpoint prompt lost exact whitespace: got %q want %q", loaded.Meta.Prompt, prompt)
	}

	if err := store.Delete(project, conversation.ID, meta.ID); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	items, err = store.List(project, conversation.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("checkpoint remains after delete: %+v err=%v", items, err)
	}
}

func TestGitWorkspaceSnapshotRestoreAndFork(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Lilith Test")
	runGit(t, repo, "config", "user.email", "lilith@example.invalid")
	// Simula la configuración común de Windows que puede convertir LF a CRLF.
	// Rewind debe restaurar los bytes capturados, no la preferencia global de Git.
	runGit(t, repo, "config", "core.autocrlf", "true")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), "ignored.txt\n")
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "base\n")
	writeTestFile(t, filepath.Join(repo, "ignored.txt"), "ignored base\n")
	runGit(t, repo, "add", ".gitignore", "tracked.txt")
	runGit(t, repo, "add", "-f", "ignored.txt")
	runGit(t, repo, "commit", "-m", "base")
	head := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	subproject := filepath.Join(repo, "nested", "app")
	if err := os.MkdirAll(subproject, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveWorkspaceRoot(subproject); filepath.Clean(got) != filepath.Clean(repo) {
		t.Fatalf("git subproject resolved to %s, want repository root %s", got, repo)
	}

	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "snapshot tracked\n")
	writeTestFile(t, filepath.Join(repo, "ignored.txt"), "snapshot ignored\n")
	writeTestFile(t, filepath.Join(repo, "untracked.txt"), "snapshot untracked\n")
	writeTestFile(t, filepath.Join(repo, "swap"), "snapshot file\n")
	// A staged path verifies that using a temporary index never modifies the
	// user's real staging area.
	writeTestFile(t, filepath.Join(repo, "staged.txt"), "staged snapshot\n")
	runGit(t, repo, "add", "staged.txt")
	indexBefore := fileDigest(t, gitIndexPath(t, repo))
	statusBefore := runGit(t, repo, "status", "--porcelain=v1")

	store := NewStore(t.TempDir())
	conversation := session.New(repo)
	conversation.Messages = []openai.Message{{Role: "user", Content: "before mutation"}}
	meta, err := store.Create(repo, conversation.ID, "mutate files", conversation)
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	meta, err = store.CaptureWorkspace(repo, conversation.ID, meta.ID)
	if err != nil {
		t.Fatalf("capture git workspace: %v", err)
	}
	if !meta.HasCode || meta.PartialCode || meta.FileCount < 5 {
		t.Fatalf("unexpected code metadata: %+v", meta)
	}
	point, err := store.Load(repo, conversation.ID, meta.ID)
	if err != nil {
		t.Fatalf("load captured point: %v", err)
	}
	if point.Workspace.Kind != workspaceGit || point.Workspace.GitCommit == "" || point.Workspace.GitRef == "" {
		t.Fatalf("expected git snapshot: %+v", point.Workspace)
	}
	parent := strings.TrimSpace(runGit(t, repo, "rev-parse", point.Workspace.GitCommit+"^"))
	if parent != head {
		t.Fatalf("snapshot commit must preserve repository history: parent=%s head=%s", parent, head)
	}
	if got := fileDigest(t, gitIndexPath(t, repo)); got != indexBefore {
		t.Fatalf("capture changed the real git index: before=%s after=%s", indexBefore, got)
	}
	if got := runGit(t, repo, "status", "--porcelain=v1"); got != statusBefore {
		t.Fatalf("capture changed git status:\nbefore:\n%s\nafter:\n%s", statusBefore, got)
	}

	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "future tracked\n")
	writeTestFile(t, filepath.Join(repo, "ignored.txt"), "future ignored\n")
	_ = os.Remove(filepath.Join(repo, "untracked.txt"))
	_ = os.Remove(filepath.Join(repo, "swap"))
	if err := os.MkdirAll(filepath.Join(repo, "swap"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "swap", "child.txt"), "future child\n")
	writeTestFile(t, filepath.Join(repo, "later.txt"), "created later\n")

	if err := store.RestoreWorkspace(point); err != nil {
		t.Fatalf("restore git workspace: %v", err)
	}
	assertFileText(t, filepath.Join(repo, "tracked.txt"), "snapshot tracked\n")
	assertFileText(t, filepath.Join(repo, "ignored.txt"), "snapshot ignored\n")
	assertFileText(t, filepath.Join(repo, "untracked.txt"), "snapshot untracked\n")
	assertFileText(t, filepath.Join(repo, "swap"), "snapshot file\n")
	if _, err := os.Stat(filepath.Join(repo, "later.txt")); !os.IsNotExist(err) {
		t.Fatalf("file created after checkpoint was not removed: %v", err)
	}
	if got := fileDigest(t, gitIndexPath(t, repo)); got != indexBefore {
		t.Fatalf("restore changed the real git index: before=%s after=%s", indexBefore, got)
	}

	forkDir := filepath.Join(t.TempDir(), "fork")
	if err := os.Mkdir(forkDir, 0o755); err != nil {
		t.Fatalf("create selected empty fork directory: %v", err)
	}
	fork, err := store.ForkWorkspace(point, forkDir)
	if err != nil {
		t.Fatalf("fork git workspace: %v", err)
	}
	if fork.Kind != workspaceGit || filepath.Clean(fork.ProjectPath) != filepath.Clean(forkDir) {
		t.Fatalf("unexpected fork result: %+v", fork)
	}
	assertFileText(t, filepath.Join(forkDir, "tracked.txt"), "snapshot tracked\n")
	assertFileText(t, filepath.Join(forkDir, "untracked.txt"), "snapshot untracked\n")
	forkHead := strings.TrimSpace(runGit(t, forkDir, "rev-parse", "HEAD"))
	if forkHead != point.Workspace.GitCommit {
		t.Fatalf("fork checkout points at %s, want %s", forkHead, point.Workspace.GitCommit)
	}
	t.Cleanup(func() {
		_, _ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", forkDir).CombinedOutput()
	})
}

func TestResolveWorkspaceRootFallsBackToProjectOutsideGit(t *testing.T) {
	project := t.TempDir()
	if got := ResolveWorkspaceRoot(project); filepath.Clean(got) != filepath.Clean(project) {
		t.Fatalf("non-Git project resolved to %s, want %s", got, project)
	}
}

func TestGitSubdirectoryRestoreDoesNotTouchSiblingWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	project := filepath.Join(repo, "app")
	sibling := filepath.Join(repo, "docs", "notes.txt")
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Lilith Test")
	runGit(t, repo, "config", "user.email", "lilith@example.invalid")
	writeTestFile(t, filepath.Join(project, "main.txt"), "base app\n")
	writeTestFile(t, sibling, "base docs\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")

	writeTestFile(t, filepath.Join(project, "main.txt"), "snapshot app\n")
	store := NewStore(t.TempDir())
	conversation := session.New(project)
	meta, err := store.Create(project, conversation.ID, "cambia app", conversation)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = store.CaptureWorkspace(project, conversation.ID, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	point, err := store.Load(project, conversation.ID, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if point.Workspace.WorkingRel != "app" {
		t.Fatalf("unexpected git scope: %q", point.Workspace.WorkingRel)
	}

	writeTestFile(t, filepath.Join(project, "main.txt"), "later app\n")
	writeTestFile(t, sibling, "later docs must survive\n")
	writeTestFile(t, filepath.Join(repo, "docs", "new.txt"), "sibling untracked must survive\n")
	if err := store.RestoreWorkspace(point); err != nil {
		t.Fatal(err)
	}
	assertFileText(t, filepath.Join(project, "main.txt"), "snapshot app\n")
	assertFileText(t, sibling, "later docs must survive\n")
	assertFileText(t, filepath.Join(repo, "docs", "new.txt"), "sibling untracked must survive\n")
}

func TestFallbackWorkspaceRestoreAndFork(t *testing.T) {
	root := t.TempDir()
	store := NewStore(t.TempDir())
	blobs := store.blobDir(root)
	writeTestFile(t, filepath.Join(root, "src", "main.txt"), "snapshot\n")
	writeTestFile(t, filepath.Join(root, "remove-me.txt"), "keep in snapshot\n")
	writeTestFile(t, filepath.Join(root, "dist", "artifact.bin"), "generated old\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink("src/main.txt", filepath.Join(root, "main-link")); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := captureFileWorkspace(root, blobs)
	if err != nil {
		t.Fatalf("capture fallback: %v", err)
	}
	if snapshot.Kind != workspaceFiles || len(snapshot.Files) < 2 {
		t.Fatalf("unexpected fallback snapshot: %+v", snapshot)
	}
	for _, file := range snapshot.Files {
		if strings.HasPrefix(file.Path, "dist/") {
			t.Fatalf("generated directory must not be included: %+v", file)
		}
	}

	writeTestFile(t, filepath.Join(root, "src", "main.txt"), "future\n")
	_ = os.Remove(filepath.Join(root, "remove-me.txt"))
	writeTestFile(t, filepath.Join(root, "later.txt"), "later\n")
	writeTestFile(t, filepath.Join(root, "dist", "artifact.bin"), "generated future\n")
	if err := restoreFileWorkspace(snapshot, blobs, root); err != nil {
		t.Fatalf("restore fallback: %v", err)
	}
	assertFileText(t, filepath.Join(root, "src", "main.txt"), "snapshot\n")
	assertFileText(t, filepath.Join(root, "remove-me.txt"), "keep in snapshot\n")
	if _, err := os.Stat(filepath.Join(root, "later.txt")); !os.IsNotExist(err) {
		t.Fatalf("later file was not removed: %v", err)
	}
	// Excluded/generated directories are intentionally left untouched.
	assertFileText(t, filepath.Join(root, "dist", "artifact.bin"), "generated future\n")

	forkRoot := filepath.Join(t.TempDir(), "fork")
	if err := os.Mkdir(forkRoot, 0o755); err != nil {
		t.Fatalf("create selected fallback directory: %v", err)
	}
	point := &Point{
		Meta:      Meta{ProjectPath: root, HasCode: true},
		Workspace: snapshot,
	}
	fork, err := store.ForkWorkspace(point, forkRoot)
	if err != nil {
		t.Fatalf("fork fallback copy: %v", err)
	}
	if fork.Kind != workspaceFiles || filepath.Clean(fork.ProjectPath) != filepath.Clean(forkRoot) {
		t.Fatalf("unexpected fallback fork result: %+v", fork)
	}
	assertFileText(t, filepath.Join(forkRoot, "src", "main.txt"), "snapshot\n")
	assertFileText(t, filepath.Join(forkRoot, "remove-me.txt"), "keep in snapshot\n")
}

func TestPrepareForkDestinationAllowsExistingEmptyDirectory(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	destination := filepath.Join(t.TempDir(), "fork")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	got, existed, err := prepareForkDestination(workspace, destination)
	if err != nil {
		t.Fatalf("empty existing destination was rejected: %v", err)
	}
	if !existed || filepath.Clean(got) != filepath.Clean(destination) {
		t.Fatalf("unexpected destination result: path=%s existed=%v", got, existed)
	}
}

func TestPrepareForkDestinationRejectsNonEmptyAndNestedDirectories(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(workspace, "fork")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareForkDestination(workspace, nested); err == nil || !strings.Contains(err.Error(), "workspace original") {
		t.Fatalf("nested destination was not rejected safely: %v", err)
	}
	if runtime.GOOS != "windows" {
		linked := filepath.Join(t.TempDir(), "linked-inside-workspace")
		if err := os.Symlink(nested, linked); err != nil {
			t.Fatal(err)
		}
		if _, _, err := prepareForkDestination(workspace, linked); err == nil || !strings.Contains(err.Error(), "workspace original") {
			t.Fatalf("symlinked nested destination was not rejected safely: %v", err)
		}
	}

	nonEmpty := filepath.Join(t.TempDir(), "non-empty")
	writeTestFile(t, filepath.Join(nonEmpty, "keep.txt"), "keep\n")
	if _, _, err := prepareForkDestination(workspace, nonEmpty); err == nil || !strings.Contains(err.Error(), "debe estar vacía") {
		t.Fatalf("non-empty destination was not rejected safely: %v", err)
	}
}

func TestCaptureWorkspaceTimeoutWhileStoreIsBusy(t *testing.T) {
	project := t.TempDir()
	store := NewStore(t.TempDir())
	conversation := session.New(project)
	meta, err := store.Create(project, conversation.ID, "checkpoint", conversation)
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	start := time.Now()
	_, err = store.CaptureWorkspaceContext(ctx, project, conversation.ID, meta.ID)
	elapsed := time.Since(start)
	cancel()
	store.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy store returned %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("busy store ignored context for %s", elapsed)
	}
}

func TestCanceledWorkspaceOperationsReturnPromptly(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "state.txt"), []byte("state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir())
	conversation := session.New(project)
	meta, err := store.Create(project, conversation.ID, "checkpoint", conversation)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CaptureWorkspaceContext(ctx, project, conversation.ID, meta.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture returned %v, want context.Canceled", err)
	}
	point, err := store.Load(project, conversation.ID, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreWorkspaceContext(ctx, point); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restore returned %v, want context.Canceled", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func gitIndexPath(t *testing.T, repo string) string {
	t.Helper()
	path := strings.TrimSpace(runGit(t, repo, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, filepath.FromSlash(path))
	}
	return path
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileText(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
