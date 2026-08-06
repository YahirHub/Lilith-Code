package shell

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lilith/li/internal/textsearch"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

const portableShellPath = "embedded:mvdan.cc/sh/v3"

// lockedBuffer is safe for background commands, which may write concurrently.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Keep one byte beyond MaxOutputBytes so shell.clip can mark the stream as
	// truncated, while still reporting a successful write to pipelines.
	remaining := MaxOutputBytes + 1 - b.b.Len()
	if remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = b.b.Write(chunk)
	}
	return len(p), nil
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []byte(b.b.String())
}

func runPortable(ctx context.Context, command, dir string) (stdout, stderr []byte, exitCode int, err error) {
	program, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "<lilith-portable>")
	if err != nil {
		return nil, nil, 2, fmt.Errorf("portable shell syntax error: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, 1, err
	}
	var outBuf, errBuf lockedBuffer
	runner, err := interp.New(
		interp.Dir(absDir),
		interp.Env(expand.ListEnviron(os.Environ()...)),
		interp.StdIO(strings.NewReader(""), &outBuf, &errBuf),
		interp.ExecHandlers(portableCommandFallback),
	)
	if err != nil {
		return nil, nil, 1, err
	}
	runErr := runner.Run(ctx, program)
	stdout, stderr = outBuf.Bytes(), errBuf.Bytes()
	if runErr == nil {
		return stdout, stderr, 0, nil
	}
	var status interp.ExitStatus
	if errors.As(runErr, &status) {
		return stdout, stderr, int(status), nil
	}
	if ctx.Err() != nil {
		return stdout, stderr, -1, nil
	}
	return stdout, stderr, 1, runErr
}

func portableCommandFallback(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return next(ctx, args)
		}
		hc := interp.HandlerCtx(ctx)
		name := strings.ToLower(filepath.Base(args[0]))
		name = strings.TrimSuffix(name, ".exe")
		if strings.ContainsAny(args[0], `/\\`) {
			return next(ctx, args)
		}
		if _, err := interp.LookPathDir(hc.Dir, hc.Env, args[0]); err == nil {
			return next(ctx, args)
		}
		handler, ok := portableCommands[name]
		if !ok {
			fmt.Fprintf(hc.Stderr, "%s: command not found (portable shell only replaces shell syntax and a curated Go toolbox; install the executable or use a Lilith tool)\n", args[0])
			return interp.ExitStatus(127)
		}
		return handler(ctx, hc, args[1:])
	}
}

type portableCommand func(context.Context, interp.HandlerContext, []string) error

var portableCommands = map[string]portableCommand{
	"rg":        portableRG,
	"ripgrep":   portableRG,
	"grep":      portableGrep,
	"find":      portableFind,
	"ls":        portableLS,
	"cat":       portableCat,
	"head":      portableHead,
	"tail":      portableTail,
	"wc":        portableWC,
	"mkdir":     portableMkdir,
	"touch":     portableTouch,
	"cp":        portableCopy,
	"mv":        portableMove,
	"rm":        portableRemove,
	"chmod":     portableChmod,
	"sha256sum": portableSHA256,
}

func portableRG(ctx context.Context, hc interp.HandlerContext, args []string) error {
	opts := textsearch.Options{Root: hc.Dir, Limit: 100, MaxLine: 500}
	var pattern string
	var targets []string
	options := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") && arg != "-" {
			switch {
			case arg == "-F" || arg == "--fixed-strings":
				opts.Literal = true
			case arg == "-i" || arg == "--ignore-case":
				opts.IgnoreCase = true
			case arg == "--hidden":
				opts.Hidden = true
			case arg == "-n" || arg == "--line-number" || arg == "--no-heading" || arg == "--heading" || arg == "--with-filename" || arg == "--no-messages":
				// The fallback always emits stable path:line:text records.
			case arg == "--color" || arg == "--max-columns" || arg == "--max-count" || arg == "-C" || arg == "--context" || arg == "-g" || arg == "--glob":
				if i+1 >= len(args) {
					return portableUsage(hc, "rg", fmt.Sprintf("%s requires a value", arg))
				}
				i++
				value := args[i]
				switch arg {
				case "-C", "--context":
					n, err := strconv.Atoi(value)
					if err != nil || n < 0 {
						return portableUsage(hc, "rg", "invalid context")
					}
					opts.Context = n
				case "-g", "--glob":
					if strings.HasPrefix(value, "!") {
						return portableUsage(hc, "rg", "negative globs require native ripgrep")
					}
					if opts.Glob != "" {
						return portableUsage(hc, "rg", "multiple globs require native ripgrep")
					}
					opts.Glob = value
				case "--max-count":
					n, err := strconv.Atoi(value)
					if err != nil || n <= 0 {
						return portableUsage(hc, "rg", "invalid max-count")
					}
					opts.Limit = n
				case "--max-columns":
					n, err := strconv.Atoi(value)
					if err != nil || n <= 0 {
						return portableUsage(hc, "rg", "invalid max-columns")
					}
					opts.MaxLine = n
				}
			case strings.HasPrefix(arg, "-C") && len(arg) > 2:
				n, err := strconv.Atoi(arg[2:])
				if err != nil || n < 0 {
					return portableUsage(hc, "rg", "invalid context")
				}
				opts.Context = n
			case strings.HasPrefix(arg, "--context="):
				n, err := strconv.Atoi(strings.TrimPrefix(arg, "--context="))
				if err != nil || n < 0 {
					return portableUsage(hc, "rg", "invalid context")
				}
				opts.Context = n
			case strings.HasPrefix(arg, "--max-count="):
				n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-count="))
				if err != nil || n <= 0 {
					return portableUsage(hc, "rg", "invalid max-count")
				}
				opts.Limit = n
			case strings.HasPrefix(arg, "--max-columns="):
				n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-columns="))
				if err != nil || n <= 0 {
					return portableUsage(hc, "rg", "invalid max-columns")
				}
				opts.MaxLine = n
			case strings.HasPrefix(arg, "--color="):
				// Output is always colorless in the portable fallback.
			case strings.HasPrefix(arg, "--glob="):
				value := strings.TrimPrefix(arg, "--glob=")
				if strings.HasPrefix(value, "!") {
					return portableUsage(hc, "rg", "negative globs require native ripgrep")
				}
				if opts.Glob != "" {
					return portableUsage(hc, "rg", "multiple globs require native ripgrep")
				}
				opts.Glob = value
			case arg == "--version":
				fmt.Fprintln(hc.Stdout, "rg (Lilith portable Go fallback)")
				return nil
			default:
				return portableUsage(hc, "rg", fmt.Sprintf("unsupported option %s", arg))
			}
			continue
		}
		if pattern == "" {
			pattern = arg
		} else {
			targets = append(targets, arg)
		}
	}
	if pattern == "" {
		return portableUsage(hc, "rg", "missing pattern")
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	total := 0
	maxMatches := opts.Limit
	for targetIndex, target := range targets {
		remaining := maxMatches - total
		if remaining <= 0 {
			fmt.Fprintf(hc.Stderr, "rg: results truncated at %d matches by portable fallback\n", maxMatches)
			break
		}
		opts.Limit = remaining
		opts.Path = resolvePortablePath(hc.Dir, target)
		res, err := textsearch.Search(ctx, opts)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "rg: %v\n", err)
			return interp.ExitStatus(2)
		}
		if res.Text != "" {
			fmt.Fprintln(hc.Stdout, res.Text)
		}
		total += res.Matches
		if res.Truncated || (total >= maxMatches && targetIndex+1 < len(targets)) {
			fmt.Fprintf(hc.Stderr, "rg: results truncated at %d matches by portable fallback\n", maxMatches)
			break
		}
	}
	if total == 0 {
		return interp.ExitStatus(1)
	}
	return nil
}

func portableGrep(ctx context.Context, hc interp.HandlerContext, args []string) error {
	literal, ignoreCase, recursive := false, false, false
	contextLines, limit := 0, 100
	var pattern string
	var targets []string
	options := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") && arg != "-" {
			switch {
			case arg == "-F" || arg == "--fixed-strings":
				literal = true
			case arg == "-i" || arg == "--ignore-case":
				ignoreCase = true
			case arg == "-r" || arg == "-R" || arg == "--recursive":
				recursive = true
			case arg == "-n" || arg == "--line-number" || arg == "-E" || arg == "--extended-regexp" || arg == "-H" || arg == "--with-filename":
			case arg == "-C" || arg == "--context" || arg == "-m" || arg == "--max-count":
				if i+1 >= len(args) {
					return portableUsage(hc, "grep", fmt.Sprintf("%s requires a value", arg))
				}
				i++
				n, err := strconv.Atoi(args[i])
				if err != nil || n < 0 {
					return portableUsage(hc, "grep", "invalid numeric option")
				}
				if arg == "-C" || arg == "--context" {
					contextLines = n
				} else if n > 0 {
					limit = n
				}
			case strings.HasPrefix(arg, "-C") && len(arg) > 2:
				n, err := strconv.Atoi(arg[2:])
				if err != nil || n < 0 {
					return portableUsage(hc, "grep", "invalid context")
				}
				contextLines = n
			default:
				return portableUsage(hc, "grep", fmt.Sprintf("unsupported option %s", arg))
			}
			continue
		}
		if pattern == "" {
			pattern = arg
		} else {
			targets = append(targets, arg)
		}
	}
	if pattern == "" {
		return portableUsage(hc, "grep", "missing pattern")
	}
	if len(targets) == 0 {
		res, err := textsearch.SearchReader(ctx, "(standard input)", hc.Stdin, pattern, literal, ignoreCase, contextLines, limit, 500)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "grep: %v\n", err)
			return interp.ExitStatus(2)
		}
		if res.Text != "" {
			fmt.Fprintln(hc.Stdout, res.Text)
		}
		if res.Matches == 0 {
			return interp.ExitStatus(1)
		}
		return nil
	}
	total := 0
	for _, target := range targets {
		remaining := limit - total
		if remaining <= 0 {
			break
		}
		full := resolvePortablePath(hc.Dir, target)
		info, err := os.Stat(full)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "grep: %s: %v\n", target, err)
			return interp.ExitStatus(2)
		}
		if info.IsDir() && !recursive {
			fmt.Fprintf(hc.Stderr, "grep: %s: is a directory\n", target)
			continue
		}
		res, err := textsearch.Search(ctx, textsearch.Options{Root: hc.Dir, Path: full, Pattern: pattern, Literal: literal, IgnoreCase: ignoreCase, Context: contextLines, Limit: remaining, MaxLine: 500, Hidden: true})
		if err != nil {
			fmt.Fprintf(hc.Stderr, "grep: %v\n", err)
			return interp.ExitStatus(2)
		}
		if res.Text != "" {
			fmt.Fprintln(hc.Stdout, res.Text)
		}
		total += res.Matches
	}
	if total == 0 {
		return interp.ExitStatus(1)
	}
	return nil
}

func portableFind(ctx context.Context, hc interp.HandlerContext, args []string) error {
	root := "."
	namePattern := ""
	typeFilter := byte(0)
	maxDepth := -1
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-name":
			if i+1 >= len(args) {
				return portableUsage(hc, "find", "-name requires a pattern")
			}
			i++
			namePattern = args[i]
		case "-type":
			if i+1 >= len(args) || (args[i+1] != "f" && args[i+1] != "d" && args[i+1] != "l") {
				return portableUsage(hc, "find", "-type supports f, d or l")
			}
			i++
			typeFilter = args[i][0]
		case "-maxdepth":
			if i+1 >= len(args) {
				return portableUsage(hc, "find", "-maxdepth requires a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return portableUsage(hc, "find", "invalid maxdepth")
			}
			maxDepth = n
		default:
			return portableUsage(hc, "find", fmt.Sprintf("unsupported expression %s", args[i]))
		}
	}
	fullRoot := resolvePortablePath(hc.Dir, root)
	baseDepth := strings.Count(filepath.Clean(fullRoot), string(filepath.Separator))
	err := filepath.WalkDir(fullRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Fprintf(hc.Stderr, "find: %v\n", walkErr)
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		depth := strings.Count(filepath.Clean(filePath), string(filepath.Separator)) - baseDepth
		if maxDepth >= 0 && depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if namePattern != "" {
			ok, _ := filepath.Match(namePattern, entry.Name())
			if !ok {
				return nil
			}
		}
		if typeFilter != 0 {
			mode := entry.Type()
			if typeFilter == 'f' && (entry.IsDir() || mode&fs.ModeSymlink != 0) {
				return nil
			}
			if typeFilter == 'd' && !entry.IsDir() {
				return nil
			}
			if typeFilter == 'l' && mode&fs.ModeSymlink == 0 {
				return nil
			}
		}
		display, err := filepath.Rel(hc.Dir, filePath)
		if err != nil || strings.HasPrefix(display, "..") {
			display = filePath
		} else if root == "." && display != "." {
			display = "." + string(filepath.Separator) + display
		}
		fmt.Fprintln(hc.Stdout, filepath.ToSlash(display))
		return nil
	})
	if err != nil {
		fmt.Fprintf(hc.Stderr, "find: %v\n", err)
		return interp.ExitStatus(1)
	}
	return nil
}

func portableLS(_ context.Context, hc interp.HandlerContext, args []string) error {
	showAll := false
	long := false
	var targets []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			for _, flag := range strings.TrimLeft(arg, "-") {
				switch flag {
				case 'a', 'A':
					showAll = true
				case 'l':
					long = true
				default:
					return portableUsage(hc, "ls", fmt.Sprintf("unsupported option %s", arg))
				}
			}
			continue
		}
		targets = append(targets, arg)
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	status := 0
	for ti, target := range targets {
		full := resolvePortablePath(hc.Dir, target)
		info, err := os.Stat(full)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "ls: %s: %v\n", target, err)
			status = 1
			continue
		}
		if len(targets) > 1 {
			if ti > 0 {
				fmt.Fprintln(hc.Stdout)
			}
			fmt.Fprintf(hc.Stdout, "%s:\n", target)
		}
		if !info.IsDir() {
			printLSEntry(hc.Stdout, info, filepath.Base(full), long)
			continue
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "ls: %s: %v\n", target, err)
			status = 1
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) })
		for _, entry := range entries {
			if !showAll && strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			entryInfo, err := entry.Info()
			if err != nil {
				continue
			}
			printLSEntry(hc.Stdout, entryInfo, entry.Name(), long)
		}
	}
	if status != 0 {
		return interp.ExitStatus(status)
	}
	return nil
}

func printLSEntry(w io.Writer, info fs.FileInfo, name string, long bool) {
	if long {
		fmt.Fprintf(w, "%s %10d %s %s\n", info.Mode().String(), info.Size(), info.ModTime().Format("2006-01-02 15:04"), name)
		return
	}
	fmt.Fprintln(w, name)
}

func portableCat(ctx context.Context, hc interp.HandlerContext, args []string) error {
	if len(args) == 0 {
		_, err := copyWithContext(ctx, hc.Stdout, hc.Stdin)
		return portableIOError(hc, "cat", err)
	}
	for _, name := range args {
		if name == "-" {
			if _, err := copyWithContext(ctx, hc.Stdout, hc.Stdin); err != nil {
				return portableIOError(hc, "cat", err)
			}
			continue
		}
		f, err := os.Open(resolvePortablePath(hc.Dir, name))
		if err != nil {
			fmt.Fprintf(hc.Stderr, "cat: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
		_, copyErr := copyWithContext(ctx, hc.Stdout, f)
		closeErr := f.Close()
		if copyErr != nil {
			return portableIOError(hc, "cat", copyErr)
		}
		if closeErr != nil {
			return portableIOError(hc, "cat", closeErr)
		}
	}
	return nil
}

func portableHead(ctx context.Context, hc interp.HandlerContext, args []string) error {
	count, files, err := parseLineCount(args, 10)
	if err != nil {
		return portableUsage(hc, "head", err.Error())
	}
	return portableLineCommand(ctx, hc, "head", files, func(lines []string) []string {
		n := count
		if n > len(lines) {
			n = len(lines)
		}
		return lines[:n]
	})
}

func portableTail(ctx context.Context, hc interp.HandlerContext, args []string) error {
	count, files, err := parseLineCount(args, 10)
	if err != nil {
		return portableUsage(hc, "tail", err.Error())
	}
	return portableLineCommand(ctx, hc, "tail", files, func(lines []string) []string {
		start := len(lines) - count
		if start < 0 {
			start = 0
		}
		return lines[start:]
	})
}

func parseLineCount(args []string, defaultCount int) (int, []string, error) {
	count := defaultCount
	var files []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-n" {
			if i+1 >= len(args) {
				return 0, nil, errors.New("-n requires a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return 0, nil, errors.New("invalid line count")
			}
			count = n
			continue
		}
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "-"))
			if err == nil && n >= 0 {
				count = n
				continue
			}
		}
		files = append(files, arg)
	}
	return count, files, nil
}

func portableLineCommand(ctx context.Context, hc interp.HandlerContext, name string, files []string, selectLines func([]string) []string) error {
	if len(files) == 0 {
		files = []string{"-"}
	}
	for i, file := range files {
		var r io.Reader = hc.Stdin
		var closer io.Closer
		if file != "-" {
			f, err := os.Open(resolvePortablePath(hc.Dir, file))
			if err != nil {
				fmt.Fprintf(hc.Stderr, "%s: %s: %v\n", name, file, err)
				return interp.ExitStatus(1)
			}
			r, closer = f, f
		}
		lines, err := readLinesLimited(ctx, r, 16<<20)
		if closer != nil {
			_ = closer.Close()
		}
		if err != nil {
			fmt.Fprintf(hc.Stderr, "%s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
		if len(files) > 1 {
			if i > 0 {
				fmt.Fprintln(hc.Stdout)
			}
			fmt.Fprintf(hc.Stdout, "==> %s <==\n", file)
		}
		selected := selectLines(lines)
		if len(selected) > 0 {
			fmt.Fprintln(hc.Stdout, strings.Join(selected, "\n"))
		}
	}
	return nil
}

func portableWC(ctx context.Context, hc interp.HandlerContext, args []string) error {
	showLines, showWords, showBytes := false, false, false
	var files []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			for _, flag := range strings.TrimLeft(arg, "-") {
				switch flag {
				case 'l':
					showLines = true
				case 'w':
					showWords = true
				case 'c':
					showBytes = true
				default:
					return portableUsage(hc, "wc", fmt.Sprintf("unsupported option %s", arg))
				}
			}
			continue
		}
		files = append(files, arg)
	}
	if !showLines && !showWords && !showBytes {
		showLines, showWords, showBytes = true, true, true
	}
	if len(files) == 0 {
		files = []string{"-"}
	}
	for _, file := range files {
		var r io.Reader = hc.Stdin
		var closer io.Closer
		if file != "-" {
			f, err := os.Open(resolvePortablePath(hc.Dir, file))
			if err != nil {
				fmt.Fprintf(hc.Stderr, "wc: %s: %v\n", file, err)
				return interp.ExitStatus(1)
			}
			r, closer = f, f
		}
		data, err := readAllLimited(ctx, r, 16<<20)
		if closer != nil {
			_ = closer.Close()
		}
		if err != nil || len(data) > 16<<20 {
			if err == nil {
				err = errors.New("input exceeds 16 MiB portable limit")
			}
			fmt.Fprintf(hc.Stderr, "wc: %s: %v\n", file, err)
			return interp.ExitStatus(1)
		}
		var fields []string
		if showLines {
			fields = append(fields, strconv.Itoa(strings.Count(string(data), "\n")))
		}
		if showWords {
			fields = append(fields, strconv.Itoa(len(strings.Fields(string(data)))))
		}
		if showBytes {
			fields = append(fields, strconv.Itoa(len(data)))
		}
		if file != "-" {
			fields = append(fields, file)
		}
		fmt.Fprintln(hc.Stdout, strings.Join(fields, " "))
	}
	return nil
}

func portableMkdir(_ context.Context, hc interp.HandlerContext, args []string) error {
	parents := false
	var paths []string
	for _, arg := range args {
		if arg == "-p" || arg == "--parents" {
			parents = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return portableUsage(hc, "mkdir", fmt.Sprintf("unsupported option %s", arg))
		}
		paths = append(paths, arg)
	}
	if len(paths) == 0 {
		return portableUsage(hc, "mkdir", "missing operand")
	}
	for _, name := range paths {
		full := resolvePortablePath(hc.Dir, name)
		var err error
		if parents {
			err = os.MkdirAll(full, 0o755)
		} else {
			err = os.Mkdir(full, 0o755)
		}
		if err != nil {
			fmt.Fprintf(hc.Stderr, "mkdir: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
	}
	return nil
}

func portableTouch(_ context.Context, hc interp.HandlerContext, args []string) error {
	if len(args) == 0 {
		return portableUsage(hc, "touch", "missing operand")
	}
	now := time.Now()
	for _, name := range args {
		if strings.HasPrefix(name, "-") {
			return portableUsage(hc, "touch", fmt.Sprintf("unsupported option %s", name))
		}
		full := resolvePortablePath(hc.Dir, name)
		f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(hc.Stderr, "touch: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
		_ = f.Close()
		if err := os.Chtimes(full, now, now); err != nil {
			fmt.Fprintf(hc.Stderr, "touch: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
	}
	return nil
}

func portableCopy(ctx context.Context, hc interp.HandlerContext, args []string) error {
	recursive := false
	var operands []string
	for _, arg := range args {
		if arg == "-r" || arg == "-R" || arg == "--recursive" {
			recursive = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return portableUsage(hc, "cp", fmt.Sprintf("unsupported option %s", arg))
		}
		operands = append(operands, arg)
	}
	if len(operands) != 2 {
		return portableUsage(hc, "cp", "requires source and destination")
	}
	src := resolvePortablePath(hc.Dir, operands[0])
	dst := resolvePortablePath(hc.Dir, operands[1])
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := copyPortablePath(ctx, src, dst, recursive); err != nil {
		fmt.Fprintf(hc.Stderr, "cp: %v\n", err)
		return interp.ExitStatus(1)
	}
	return nil
}

func portableMove(ctx context.Context, hc interp.HandlerContext, args []string) error {
	if len(args) != 2 {
		return portableUsage(hc, "mv", "requires source and destination")
	}
	src := resolvePortablePath(hc.Dir, args[0])
	dst := resolvePortablePath(hc.Dir, args[1])
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		fmt.Fprintf(hc.Stderr, "mv: %v\n", err)
		return interp.ExitStatus(1)
	}
	if err := copyPortablePath(ctx, src, dst, info.IsDir()); err != nil {
		fmt.Fprintf(hc.Stderr, "mv: %v\n", err)
		return interp.ExitStatus(1)
	}
	if err := os.RemoveAll(src); err != nil {
		fmt.Fprintf(hc.Stderr, "mv: %v\n", err)
		return interp.ExitStatus(1)
	}
	return nil
}

func portableRemove(_ context.Context, hc interp.HandlerContext, args []string) error {
	recursive, force := false, false
	var paths []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			recursive = recursive || strings.ContainsAny(arg, "rR")
			force = force || strings.Contains(arg, "f")
			for _, flag := range strings.TrimLeft(arg, "-") {
				if flag != 'r' && flag != 'R' && flag != 'f' {
					return portableUsage(hc, "rm", fmt.Sprintf("unsupported option %s", arg))
				}
			}
			continue
		}
		paths = append(paths, arg)
	}
	if len(paths) == 0 {
		return portableUsage(hc, "rm", "missing operand")
	}
	for _, name := range paths {
		full := resolvePortablePath(hc.Dir, name)
		info, err := os.Lstat(full)
		if err != nil {
			if force && errors.Is(err, os.ErrNotExist) {
				continue
			}
			fmt.Fprintf(hc.Stderr, "rm: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
		if info.IsDir() && !recursive {
			fmt.Fprintf(hc.Stderr, "rm: %s: is a directory\n", name)
			return interp.ExitStatus(1)
		}
		if recursive {
			err = os.RemoveAll(full)
		} else {
			err = os.Remove(full)
		}
		if err != nil && !force {
			fmt.Fprintf(hc.Stderr, "rm: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
	}
	return nil
}

func portableChmod(_ context.Context, hc interp.HandlerContext, args []string) error {
	if len(args) < 2 {
		return portableUsage(hc, "chmod", "requires MODE and FILE")
	}
	mode, err := strconv.ParseUint(args[0], 8, 32)
	if err != nil {
		return portableUsage(hc, "chmod", "portable fallback supports octal modes only")
	}
	for _, name := range args[1:] {
		if err := os.Chmod(resolvePortablePath(hc.Dir, name), fs.FileMode(mode)); err != nil {
			fmt.Fprintf(hc.Stderr, "chmod: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
	}
	return nil
}

func portableSHA256(ctx context.Context, hc interp.HandlerContext, args []string) error {
	if len(args) == 0 {
		data, err := readAllLimited(ctx, hc.Stdin, 64<<20)
		if err != nil {
			return portableIOError(hc, "sha256sum", err)
		}
		if len(data) > 64<<20 {
			fmt.Fprintln(hc.Stderr, "sha256sum: standard input exceeds portable 64 MiB limit")
			return interp.ExitStatus(1)
		}
		fmt.Fprintf(hc.Stdout, "%x  -\n", sha256.Sum256(data))
		return nil
	}
	for _, name := range args {
		f, err := os.Open(resolvePortablePath(hc.Dir, name))
		if err != nil {
			fmt.Fprintf(hc.Stderr, "sha256sum: %s: %v\n", name, err)
			return interp.ExitStatus(1)
		}
		h := sha256.New()
		_, copyErr := copyWithContext(ctx, h, f)
		_ = f.Close()
		if copyErr != nil {
			return portableIOError(hc, "sha256sum", copyErr)
		}
		fmt.Fprintf(hc.Stdout, "%x  %s\n", h.Sum(nil), name)
	}
	return nil
}

func portableUsage(hc interp.HandlerContext, name, message string) error {
	fmt.Fprintf(hc.Stderr, "%s: %s\n", name, message)
	return interp.ExitStatus(2)
}

func portableIOError(hc interp.HandlerContext, name string, err error) error {
	if err == nil {
		return nil
	}
	fmt.Fprintf(hc.Stderr, "%s: %v\n", name, err)
	return interp.ExitStatus(1)
}

func resolvePortablePath(dir, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(dir, name)
}

func readLinesLimited(ctx context.Context, r io.Reader, maxBytes int64) ([]string, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, maxBytes+1))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	lines := make([]string, 0, 128)
	var consumed int64
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		consumed += int64(len(scanner.Bytes()) + 1)
		if consumed > maxBytes {
			return nil, fmt.Errorf("input exceeds %d bytes", maxBytes)
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func readAllLimited(ctx context.Context, r io.Reader, maxBytes int64) ([]byte, error) {
	var buf strings.Builder
	_, err := copyWithContext(ctx, &buf, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	data := []byte(buf.String())
	if int64(len(data)) > maxBytes {
		return data, fmt.Errorf("input exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func copyPortablePath(ctx context.Context, src, dst string, recursive bool) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !recursive {
			return fmt.Errorf("%s is a directory (use -r)", src)
		}
		rel, err := filepath.Rel(src, dst)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return fmt.Errorf("cannot copy a directory into itself: %s -> %s", src, dst)
		}
		return filepath.WalkDir(src, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			rel, err := filepath.Rel(src, current)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)
			if entry.IsDir() {
				entryInfo, err := entry.Info()
				if err != nil {
					return err
				}
				return os.MkdirAll(target, entryInfo.Mode().Perm())
			}
			return copyPortableFile(ctx, current, target)
		})
	}
	return copyPortableFile(ctx, src, dst)
}

func copyPortableFile(ctx context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(info, dstInfo) {
		return fmt.Errorf("source and destination are the same file: %s", src)
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
	_, copyErr := copyWithContext(ctx, out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
