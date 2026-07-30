package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lilith/li/internal/hooks"
)

func createWorktree(ctx context.Context, configDir, repoRoot, taskID string, trusted bool, runner *hooks.Runner) (string, bool, error) {
	if runner != nil && runner.Has("WorktreeCreate") {
		res, err := runner.Run(ctx, "WorktreeCreate", "", map[string]any{
			"session_id":      taskID,
			"cwd":             repoRoot,
			"hook_event_name": "WorktreeCreate",
			"name":            taskID,
		})
		if err != nil {
			return "", false, fmt.Errorf("WorktreeCreate hook: %w", err)
		}
		dir := strings.TrimSpace(res.WorktreePath)
		if dir == "" || !filepath.IsAbs(dir) {
			return "", false, fmt.Errorf("WorktreeCreate hook must return an absolute worktree path")
		}
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			return "", false, fmt.Errorf("WorktreeCreate hook returned an unavailable directory: %s", dir)
		}
		// Claude's WorktreeCreate hook replaces the default git behavior, so
		// .worktreeinclude/sparse/symlink processing intentionally does not run.
		return filepath.Clean(dir), true, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", false, fmt.Errorf("isolation worktree requires git: %w", err)
	}
	if !isGitRepo(ctx, repoRoot) {
		return "", false, fmt.Errorf("isolation worktree requires a git repository")
	}
	base := filepath.Join(configDir, "worktrees")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", false, err
	}
	dir := filepath.Join(base, taskID)
	_ = os.RemoveAll(dir)
	wtSettings := loadWorktreeSettings(configDir, repoRoot, trusted)
	ref := defaultBranchRef(ctx, repoRoot)
	if wtSettings.BaseRef == "head" {
		ref = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", dir, ref)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", false, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cleanupOnError := func(err error) (string, bool, error) {
		_ = exec.CommandContext(context.Background(), "git", "worktree", "remove", "--force", dir).Run()
		return "", false, err
	}
	if err := applySparseCheckout(ctx, dir, wtSettings.SparsePaths); err != nil {
		return cleanupOnError(fmt.Errorf("configure sparse worktree: %w", err))
	}
	if err := copyWorktreeIncludes(ctx, repoRoot, dir); err != nil {
		return cleanupOnError(fmt.Errorf("copy .worktreeinclude: %w", err))
	}
	if err := applyWorktreeSymlinks(repoRoot, dir, wtSettings.SymlinkDirectories); err != nil {
		return cleanupOnError(fmt.Errorf("configure worktree symlinks: %w", err))
	}
	return dir, false, nil
}

type worktreeSettings struct {
	BaseRef            string
	SymlinkDirectories []string
	SparsePaths        []string
}

func loadWorktreeSettings(configDir, repoRoot string, trusted bool) worktreeSettings {
	settings := worktreeSettings{BaseRef: "fresh"}
	home := filepath.Dir(filepath.Clean(configDir))
	paths := []string{}
	if strings.TrimSpace(configDir) != "" {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if trusted {
		paths = append(paths, filepath.Join(repoRoot, ".claude", "settings.json"), filepath.Join(repoRoot, ".claude", "settings.local.json"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			Worktree struct {
				BaseRef            string   `json:"baseRef"`
				SymlinkDirectories []string `json:"symlinkDirectories"`
				SparsePaths        []string `json:"sparsePaths"`
			} `json:"worktree"`
		}
		if json.Unmarshal(data, &raw) != nil {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(raw.Worktree.BaseRef))
		if v == "fresh" || v == "head" {
			settings.BaseRef = v
		}
		if raw.Worktree.SymlinkDirectories != nil {
			settings.SymlinkDirectories = cleanRelativeWorktreePaths(raw.Worktree.SymlinkDirectories)
		}
		if raw.Worktree.SparsePaths != nil {
			settings.SparsePaths = cleanRelativeWorktreePaths(raw.Worktree.SparsePaths)
		}
	}
	return settings
}

func cleanRelativeWorktreePaths(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range values {
		v := filepath.Clean(filepath.FromSlash(strings.TrimSpace(raw)))
		if v == "." || v == "" || filepath.IsAbs(v) || v == ".." || strings.HasPrefix(v, ".."+string(filepath.Separator)) {
			continue
		}
		v = filepath.ToSlash(v)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func applySparseCheckout(ctx context.Context, worktree string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	initCmd := exec.CommandContext(ctx, "git", "sparse-checkout", "init", "--cone")
	initCmd.Dir = worktree
	if out, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git sparse-checkout init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	args := append([]string{"sparse-checkout", "set", "--cone", "--"}, paths...)
	setCmd := exec.CommandContext(ctx, "git", args...)
	setCmd.Dir = worktree
	if out, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git sparse-checkout set: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func applyWorktreeSymlinks(repoRoot, worktree string, paths []string) error {
	for _, rel := range paths {
		src := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, err := os.Stat(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.IsDir() {
			continue
		}
		dst := filepath.Join(worktree, filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); err == nil {
			// Never replace tracked or copied worktree content with a symlink.
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s: %w", rel, err)
		}
	}
	return nil
}

func cleanupWorktree(ctx context.Context, repoRoot, dir string, custom bool, runner *hooks.Runner) (bool, error) {
	if strings.TrimSpace(dir) == "" {
		return false, nil
	}
	if custom {
		if runner == nil || !runner.Has("WorktreeRemove") {
			return false, nil
		}
		_, err := runner.Run(ctx, "WorktreeRemove", "", map[string]any{
			"cwd":             repoRoot,
			"hook_event_name": "WorktreeRemove",
			"worktree_path":   dir,
		})
		if err != nil {
			// WorktreeRemove cannot block. Preserve the path on failure instead of
			// guessing how a custom VCS checkout should be destroyed.
			return false, nil
		}
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return true, nil
		}
		return false, nil
	}

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(out)) != "" {
		return false, nil
	}
	cmd = exec.CommandContext(ctx, "git", "worktree", "remove", "--force", dir)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if runner != nil && runner.Has("WorktreeRemove") {
		// For normal git worktrees this is an auxiliary lifecycle event. Claude
		// documents WorktreeRemove as non-blocking, so cleanup hook failures are
		// deliberately ignored after git has removed the checkout.
		_, _ = runner.Run(context.Background(), "WorktreeRemove", "", map[string]any{
			"cwd":             repoRoot,
			"hook_event_name": "WorktreeRemove",
			"worktree_path":   dir,
		})
	}
	return true, nil
}

// validateWorktreeCommand is a second containment boundary for isolated agents.
// File tools are rooted at the worktree already; shell is trickier because git
// can redirect itself into another checkout. Fail closed on the redirection
// forms Claude Code documents and on direct references to the main checkout.
func validateWorktreeCommand(command, mainCheckout string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	lower := strings.ToLower(command)
	for _, marker := range []string{"git -c", "git --git-dir", "--git-dir", "git_dir=", "git_work_tree=", "git -c safe.directory", "git -c core.worktree", "git -c worktree"} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("worktree isolation blocks shell redirection through %s", marker)
		}
	}
	// git -C can point at an arbitrary checkout and bypass the tool cwd.
	if regexp.MustCompile(`(?i)(^|[;&|]\s*)git\s+-C(?:\s|=)`).MatchString(command) {
		return fmt.Errorf("worktree isolation blocks git -C; run git in the worktree cwd")
	}
	mainAbs, _ := filepath.Abs(mainCheckout)
	if mainAbs != "" {
		variants := []string{filepath.Clean(mainAbs), filepath.ToSlash(filepath.Clean(mainAbs))}
		for _, v := range variants {
			if v != "." && v != string(filepath.Separator) && containsPathToken(command, v) {
				return fmt.Errorf("worktree isolation blocks references to the main checkout: %s", v)
			}
		}
	}
	return nil
}

func containsPathToken(command, path string) bool {
	if path == "" {
		return false
	}
	return strings.Contains(strings.ToLower(command), strings.ToLower(path))
}

// copyWorktreeIncludes implements Claude's .worktreeinclude contract: patterns
// use gitignore-like globs and only files that are themselves gitignored can be
// copied. Tracked files are therefore never duplicated over the fresh checkout.
func copyWorktreeIncludes(ctx context.Context, repoRoot, worktree string) error {
	data, err := os.ReadFile(filepath.Join(repoRoot, ".worktreeinclude"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	patterns := parseIncludePatterns(string(data))
	if len(patterns) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(string(out), "\x00") {
		rel := filepath.ToSlash(strings.TrimSpace(raw))
		if rel == "" || !includedByPatterns(rel, patterns) {
			continue
		}
		src := filepath.Join(repoRoot, filepath.FromSlash(rel))
		dst := filepath.Join(worktree, filepath.FromSlash(rel))
		if err := copyRegularFile(src, dst); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	return nil
}

type includePattern struct {
	negative bool
	re       *regexp.Regexp
}

func parseIncludePatterns(text string) []includePattern {
	var out []includePattern
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		if neg {
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if line == "" {
			continue
		}
		out = append(out, includePattern{negative: neg, re: gitignoreLikeRegexp(line)})
	}
	return out
}

func includedByPatterns(rel string, patterns []includePattern) bool {
	matched := false
	for _, p := range patterns {
		if p.re != nil && p.re.MatchString(rel) {
			matched = !p.negative
		}
	}
	return matched
}

func gitignoreLikeRegexp(pattern string) *regexp.Regexp {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	dirOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("(?:^|.*/)")
	}
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if dirOnly {
		b.WriteString("(?:/.*)?")
	}
	b.WriteString("$")
	re, _ := regexp.Compile(b.String())
	return re
}

func copyRegularFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}

func isGitRepo(ctx context.Context, root string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func defaultBranchRef(ctx context.Context, root string) string {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return strings.TrimSpace(string(out))
	}
	return "HEAD"
}
