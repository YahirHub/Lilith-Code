package rewind

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func captureGitWorkspace(projectPath, tempDir, sessionID, pointID string) (WorkspaceSnapshot, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	repoRoot, err := gitOutput(projectPath, nil, gitPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if repoRoot == "" {
		return WorkspaceSnapshot{}, errors.New("git repository root is empty")
	}
	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	workingRel, err := filepath.Rel(repoAbs, projectAbs)
	if err != nil || workingRel == ".." || strings.HasPrefix(workingRel, ".."+string(filepath.Separator)) {
		return WorkspaceSnapshot{}, errors.New("project is outside the detected git repository")
	}
	if err := os.MkdirAll(tempDir, dirMode); err != nil {
		return WorkspaceSnapshot{}, err
	}
	index, err := os.CreateTemp(tempDir, "rewind-index-*")
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	indexPath := index.Name()
	_ = index.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if err := seedTemporaryIndex(repoRoot, indexPath, gitPath, env); err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("prepare rewind index: %w", err)
	}
	scope := filepath.ToSlash(filepath.Clean(workingRel))
	if scope == "" {
		scope = "."
	}
	if _, err := gitOutput(repoRoot, env, gitPath, gitExactContentArgs("add", "-A", "--", scope)...); err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("snapshot working tree: %w", err)
	}
	tree, err := gitOutput(repoRoot, env, gitPath, "write-tree")
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("write rewind tree: %w", err)
	}
	tree = strings.TrimSpace(tree)
	commitArgs := []string{"commit-tree", tree}
	if head, headErr := gitOutput(repoRoot, nil, gitPath, "rev-parse", "--verify", "HEAD"); headErr == nil && strings.TrimSpace(head) != "" {
		commitArgs = append(commitArgs, "-p", strings.TrimSpace(head))
	}
	commit, err := gitOutputWithInput(repoRoot, append(env,
		"GIT_AUTHOR_NAME=Lilith Rewind",
		"GIT_AUTHOR_EMAIL=rewind@localhost",
		"GIT_COMMITTER_NAME=Lilith Rewind",
		"GIT_COMMITTER_EMAIL=rewind@localhost",
	), "Lilith rewind checkpoint\n", gitPath, commitArgs...)
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("create rewind commit: %w", err)
	}
	commit = strings.TrimSpace(commit)
	ref := "refs/lilith/rewind/" + cleanGitRefPart(sessionID) + "/" + cleanGitRefPart(pointID)
	if _, err := gitOutput(repoRoot, nil, gitPath, "update-ref", ref, commit); err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("pin rewind commit: %w", err)
	}
	return WorkspaceSnapshot{
		Kind:       workspaceGit,
		Root:       repoRoot,
		WorkingRel: filepath.ToSlash(workingRel),
		GitCommit:  commit,
		GitRef:     ref,
	}, nil
}

func seedTemporaryIndex(repoRoot, indexPath, gitPath string, env []string) error {
	indexLocation, err := gitOutput(repoRoot, nil, gitPath, "rev-parse", "--git-path", "index")
	if err == nil {
		indexLocation = strings.TrimSpace(indexLocation)
		if indexLocation != "" && !filepath.IsAbs(indexLocation) {
			indexLocation = filepath.Join(repoRoot, filepath.FromSlash(indexLocation))
		}
		if in, openErr := os.Open(indexLocation); openErr == nil {
			defer in.Close()
			if copyErr := copyTo(indexPath, in); copyErr == nil {
				return nil
			}
		}
	}
	if _, headErr := gitOutput(repoRoot, nil, gitPath, "rev-parse", "--verify", "HEAD"); headErr == nil {
		_, err = gitOutput(repoRoot, env, gitPath, "read-tree", "HEAD")
		return err
	}
	_, err = gitOutput(repoRoot, env, gitPath, "read-tree", "--empty")
	return err
}

func cleanGitRefPart(raw string) string {
	v := cleanID(raw)
	v = strings.ReplaceAll(v, "..", "-")
	v = strings.Trim(v, ".")
	if v == "" {
		return "unknown"
	}
	return v
}

func deleteGitRef(repoRoot, ref string) error {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(ref) == "" {
		return nil
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	_, err = gitOutput(repoRoot, nil, gitPath, "update-ref", "-d", ref)
	return err
}

func gitFileCount(repoRoot, commit, workingRel string) (int, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return 0, err
	}
	args := []string{"ls-tree", "-r", "-z", "--name-only", commit}
	if scope := filepath.ToSlash(filepath.Clean(workingRel)); scope != "." && scope != "" {
		args = append(args, "--", scope)
	}
	out, err := gitOutputBytes(repoRoot, nil, nil, gitPath, args...)
	if err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, nil
	}
	return bytes.Count(out, []byte{0}), nil
}

func restoreGitWorkspace(snapshot WorkspaceSnapshot) error {
	if snapshot.GitCommit == "" || snapshot.Root == "" {
		return errors.New("invalid git rewind snapshot")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	if _, err := gitOutput(snapshot.Root, nil, gitPath, "cat-file", "-e", snapshot.GitCommit+"^{commit}"); err != nil {
		return fmt.Errorf("rewind commit is unavailable: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "lilith-rewind-index-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	index, err := os.CreateTemp(tempDir, "index-*")
	if err != nil {
		return err
	}
	indexPath := index.Name()
	_ = index.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitOutput(snapshot.Root, env, gitPath, "read-tree", snapshot.GitCommit); err != nil {
		return fmt.Errorf("read rewind tree: %w", err)
	}

	scope := filepath.ToSlash(filepath.Clean(snapshot.WorkingRel))
	targetArgs := []string{"ls-tree", "-r", "-z", "--name-only", snapshot.GitCommit}
	currentArgs := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard"}
	if scope != "." && scope != "" {
		targetArgs = append(targetArgs, "--", scope)
		currentArgs = append(currentArgs, "--", scope)
	}
	targetRaw, err := gitOutputBytes(snapshot.Root, nil, nil, gitPath, targetArgs...)
	if err != nil {
		return fmt.Errorf("list rewind tree: %w", err)
	}
	currentRaw, err := gitOutputBytes(snapshot.Root, nil, nil, gitPath, currentArgs...)
	if err != nil {
		return fmt.Errorf("list current workspace: %w", err)
	}
	target := nulSet(targetRaw)
	current := nulList(currentRaw)
	for _, rel := range current {
		if _, ok := target[rel]; ok {
			continue
		}
		path, safeErr := safeJoin(snapshot.Root, rel)
		if safeErr != nil {
			return safeErr
		}
		info, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.IsDir() {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", rel, err)
		}
	}
	// A snapshot may replace a directory with a file. Remove empty parent
	// directories before checkout-index so Git can materialize that file.
	removeEmptyParents(snapshot.Root, current)
	if len(targetRaw) > 0 {
		if _, err := gitOutputWithInput(snapshot.Root, env, string(targetRaw), gitPath, gitExactContentArgs("checkout-index", "--force", "-z", "--stdin")...); err != nil {
			return fmt.Errorf("restore rewind tree: %w", err)
		}
	}
	return nil
}

func forkGitWorkspace(snapshot WorkspaceSnapshot, destination string) (ForkResult, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return ForkResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return ForkResult{}, err
	}
	out, err := gitOutput(snapshot.Root, nil, gitPath, gitExactContentArgs("worktree", "add", "--detach", destination, snapshot.GitCommit)...)
	if err != nil {
		return ForkResult{}, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(out))
	}
	projectPath := destination
	if rel := filepath.Clean(filepath.FromSlash(snapshot.WorkingRel)); rel != "." && rel != "" {
		projectPath = filepath.Join(destination, rel)
	}
	return ForkResult{Root: destination, ProjectPath: projectPath, Kind: workspaceGit}, nil
}

func nulList(raw []byte) []string {
	parts := bytes.Split(raw, []byte{0})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		out = append(out, filepath.ToSlash(string(part)))
	}
	return out
}

func nulSet(raw []byte) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range nulList(raw) {
		out[item] = struct{}{}
	}
	return out
}

func safeJoin(root, rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe snapshot path: %q", rel)
	}
	path := filepath.Join(root, rel)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("snapshot path escapes workspace: %q", rel)
	}
	return pathAbs, nil
}

func removeEmptyParents(root string, paths []string) {
	seen := map[string]bool{}
	var dirs []string
	for _, rel := range paths {
		dir := filepath.Dir(filepath.FromSlash(rel))
		for dir != "." && dir != "" {
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(filepath.Separator)) > strings.Count(dirs[j], string(filepath.Separator))
	})
	for _, rel := range dirs {
		path, err := safeJoin(root, rel)
		if err == nil {
			_ = os.Remove(path)
		}
	}
}

// gitExactContentArgs disables Git's global core.autocrlf conversion for
// snapshot materialization. Rewind checkpoints must restore the exact bytes
// captured from the workspace, regardless of the user's platform-level Git
// configuration. Repository attributes remain authoritative.
func gitExactContentArgs(args ...string) []string {
	prefix := []string{"-c", "core.autocrlf=false", "-c", "core.safecrlf=false"}
	return append(prefix, args...)
}

func gitOutput(dir string, extraEnv []string, gitPath string, args ...string) (string, error) {
	out, err := gitOutputBytes(dir, extraEnv, nil, gitPath, args...)
	return string(out), err
}

func gitOutputWithInput(dir string, extraEnv []string, input string, gitPath string, args ...string) (string, error) {
	out, err := gitOutputBytes(dir, extraEnv, strings.NewReader(input), gitPath, args...)
	return string(out), err
}

func gitOutputBytes(dir string, extraEnv []string, input *strings.Reader, gitPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	if input != nil {
		cmd.Stdin = input
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message != "" {
			return stdout.Bytes(), fmt.Errorf("%w: %s", err, message)
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}
