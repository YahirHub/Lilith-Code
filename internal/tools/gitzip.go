package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/gitzip"
	"github.com/lilith/li/internal/sshremote"
)

var safeTarArg = regexp.MustCompile(`^(?:--numeric-owner|--(?:no-)?xattrs|--(?:no-)?acls|--(?:no-)?selinux|--sparse|--ignore-failed-read|--hard-dereference|--dereference|--format=(?:gnu|pax|posix|ustar)|--sort=(?:name|none)|--owner=[0-9]+|--group=[0-9]+|--mode=[A-Za-z0-9,+\-=]+|--mtime=[^\r\n\x00]+|--warning=[A-Za-z0-9_-]+)$`)
var safeZipArg = regexp.MustCompile(`^(?:-[0-9]|-X|-y|-q|-v)$`)

func mergeStringSlices(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func init() {
	register(Definition{
		Name:          "gitzip",
		Description:   "Create Git-aware ZIP/TAR/TAR.GZ deployment archives locally or through an existing ssh_remote connection. source_path may point to any specific folder to compress. include_paths can select only particular source-relative files/folders, while exclude_paths omits explicit paths or glob patterns in addition to root/nested ignore files. Always excludes .git, the archive itself, temporary manifests and protected .env files unless separately authorized.",
		PromptSnippet: "Create, upload, build remotely or extract Git-aware project archives",
		PromptGuidelines: []string{
			"Use gitzip instead of raw zip/tar when packaging a project so ignored files and .git are excluded.",
			"Set source_path to the exact local or remote folder that should become the archive root; use include_paths/exclude_paths for narrower selection.",
			"Open an SSH connection with ssh_remote first, then reuse its connection_id for upload/remote_create/remote_extract.",
			"Never set include_protected_env unless the user explicitly requests credentials/configuration secrets in the archive.",
		},
		Parameters: gitzipSchema(),
		Mutating:   true,
		Run:        runGitZip,
	})
}

func gitzipSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"action":      map[string]any{"type": "string", "enum": []string{"create", "upload", "remote_create", "remote_extract"}},
		"source_path": map[string]any{"type": "string", "description": "Exact folder to compress for create/upload/remote_create, or archive path for remote_extract. Relative local paths resolve from the project; relative remote paths resolve from the SSH connection cwd."},
		"output_path": map[string]any{"type": "string", "description": "Optional archive destination. Relative paths are resolved inside source_path."}, "format": map[string]any{"type": "string", "enum": []string{"zip", "tar", "tar.gz"}},
		"connection_id": map[string]any{"type": "string"}, "remote_path": map[string]any{"type": "string"}, "extract_remote": map[string]any{"type": "boolean"}, "extract_path": map[string]any{"type": "string"},
		"cleanup_local": map[string]any{"type": "boolean"}, "cleanup_remote_archive": map[string]any{"type": "boolean"},
		"include_paths":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 200, "description": "Optional source-relative paths/globs to include exclusively, e.g. [\"cmd/\", \"README.md\", \"assets/**/*.png\"]. Empty includes everything."},
		"exclude_paths":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 200, "description": "Additional source-relative files, folders or gitignore-style globs to omit, e.g. [\"data/\", \"*.log\", \"tmp/cache.db\"]."},
		"extra_excludes":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 200, "description": "Backward-compatible alias of exclude_paths."},
		"include_protected_env": map[string]any{"type": "boolean"}, "overwrite": map[string]any{"type": "boolean"}, "compression_level": map[string]any{"type": "integer", "minimum": 0, "maximum": 9},
		"archive_args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 40}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 86400}, "reason": map[string]any{"type": "string"},
	}, "required": []string{"action", "source_path"}, "allOf": []any{
		map[string]any{"if": map[string]any{"properties": map[string]any{"action": map[string]any{"enum": []string{"upload", "remote_create", "remote_extract"}}}}, "then": map[string]any{"required": []string{"connection_id"}}},
	}}
}

func runGitZip(ctx context.Context, args map[string]any, env Env) (string, error) {
	action := strings.TrimSpace(str(args, "action"))
	source := strings.TrimSpace(str(args, "source_path"))
	if action == "" || source == "" {
		return "", errors.New("action and source_path are required")
	}
	if len(stringSliceArg(args, "archive_args")) > 0 && action != "remote_create" {
		return "", errors.New("archive_args sólo se admite con action=remote_create")
	}
	if boolArgOr(args, "extract_remote", false) && action != "upload" {
		return "", errors.New("extract_remote sólo se admite con action=upload")
	}
	if strings.TrimSpace(str(args, "extract_path")) != "" && action != "upload" && action != "remote_extract" {
		return "", errors.New("extract_path sólo se admite con action=upload o action=remote_extract")
	}
	if boolArgOr(args, "cleanup_local", false) && action != "upload" {
		return "", errors.New("cleanup_local sólo se admite con action=upload")
	}
	if boolArgOr(args, "cleanup_remote_archive", false) && action != "upload" && action != "remote_extract" {
		return "", errors.New("cleanup_remote_archive sólo se admite con action=upload o action=remote_extract")
	}
	settings, _ := config.Load(env.ConfigDir)
	includeEnv := boolArgOr(args, "include_protected_env", false)
	if includeEnv && settings.ProtectEnvFiles {
		if err := authorizeProtectedEnv(ctx, env, "Incluir archivos .env en GitZip", source); err != nil {
			return "", err
		}
	}
	extra := mergeStringSlices(stringSliceArg(args, "exclude_paths"), stringSliceArg(args, "extra_excludes"))
	includes := stringSliceArg(args, "include_paths")
	format := gitzip.InferFormat(str(args, "format"), str(args, "output_path"), gitzip.FormatZIP)
	switch action {
	case "create":
		return createLocalGitZip(ctx, env.Root, source, str(args, "output_path"), format, extra, includes, includeEnv, boolArgOr(args, "overwrite", false), intArgOr(args, "compression_level", -1))
	case "upload":
		return uploadGitZip(ctx, args, env, source, format, extra, includes, includeEnv)
	case "remote_create":
		return remoteCreateGitZip(ctx, args, env, source, format, extra, includes, includeEnv)
	case "remote_extract":
		return remoteExtractGitZip(ctx, args, env, source, format)
	default:
		return "", fmt.Errorf("unsupported gitzip action: %s", action)
	}
}

func createLocalGitZip(ctx context.Context, projectRoot, source, output string, format gitzip.Format, extra, includes []string, includeEnv, overwrite bool, level int) (string, error) {
	if !filepath.IsAbs(source) {
		source = filepath.Join(projectRoot, source)
	}
	started := time.Now()
	res, err := gitzip.Create(ctx, gitzip.Options{SourceRoot: source, OutputPath: output, Format: format, ExtraExcludes: extra, IncludePaths: includes, IncludeProtectedEnv: includeEnv, Overwrite: overwrite, CompressionLevel: level})
	if err != nil {
		return "", err
	}
	out, err := localArchiveOutput(res, time.Since(started))
	if err != nil {
		return "", err
	}
	return jsonOutput(out)
}

func localArchiveOutput(res gitzip.Result, duration time.Duration) (map[string]any, error) {
	info, err := os.Stat(res.OutputPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "action": "create", "format": res.Format,
		"source_path": res.SourcePath, "output_path": res.OutputPath,
		"archive_bytes": info.Size(), "source_bytes": res.Bytes,
		"files": res.Files, "directories": res.Directories, "symlinks": res.Symlinks,
		"ignored_entries": res.Ignored, "protected_env_excluded": res.ProtectedEnvExcluded,
		"duration_ms": duration.Milliseconds(),
		"message":     "Project archive created using root and nested gitignore rules.",
	}, nil
}

func uploadGitZip(ctx context.Context, args map[string]any, env Env, source string, format gitzip.Format, extra, includes []string, includeEnv bool) (string, error) {
	manager := sshremote.GetManager(env.ConfigDir, env.RequestSecret, env.Confirm)
	conn, err := manager.Get(requiredArg(args, "connection_id"))
	if err != nil {
		return "", err
	}
	unlock := conn.LockOperation()
	defer unlock()
	output := str(args, "output_path")
	if !filepath.IsAbs(source) {
		source = filepath.Join(env.Root, source)
	}
	started := time.Now()
	localRes, err := gitzip.Create(ctx, gitzip.Options{SourceRoot: source, OutputPath: output, Format: format, ExtraExcludes: extra, IncludePaths: includes, IncludeProtectedEnv: includeEnv, Overwrite: boolArgOr(args, "overwrite", false), CompressionLevel: intArgOr(args, "compression_level", -1)})
	if err != nil {
		return "", err
	}
	localOutput, err := localArchiveOutput(localRes, time.Since(started))
	if err != nil {
		return "", err
	}
	remotePath := strings.TrimSpace(str(args, "remote_path"))
	if remotePath == "" {
		remotePath = filepath.Base(localRes.OutputPath)
	}
	if err = conn.Upload(localRes.OutputPath, remotePath, boolArgOr(args, "overwrite", false)); err != nil {
		return "", err
	}
	remoteResolved := conn.Resolve(remotePath)
	uploadedBytes := int64(0)
	if remoteInfo, statErr := conn.Stat(remoteResolved); statErr == nil {
		uploadedBytes = remoteInfo.Size
	}
	var extraction any
	if boolArgOr(args, "extract_remote", false) {
		extractPath := str(args, "extract_path")
		if extractPath == "" {
			extractPath = path.Dir(remoteResolved)
		}
		extraction, err = extractRemote(ctx, conn, remoteResolved, extractPath, format, boolArgOr(args, "overwrite", false), secondsArg(args, "timeout_seconds", 600))
		if err != nil {
			return "", err
		}
		if boolArgOr(args, "cleanup_remote_archive", false) {
			if err = conn.Delete(remoteResolved, false); err != nil {
				return "", err
			}
		}
	}
	if boolArgOr(args, "cleanup_local", false) {
		if err = os.Remove(localRes.OutputPath); err != nil {
			return "", err
		}
	}
	out := map[string]any{
		"ok": true, "action": "upload", "format": localRes.Format,
		"local_archive": localOutput, "connection_id": conn.ID, "remote_path": remoteResolved,
		"uploaded_bytes": uploadedBytes, "local_archive_deleted": boolArgOr(args, "cleanup_local", false),
		"message": "Gitignore-aware project archive uploaded through the persistent SSH connection.",
	}
	if extraction != nil {
		out["extraction"] = extraction
		out["message"] = "Gitignore-aware project archive uploaded and extracted remotely."
	}
	return jsonOutput(out)
}

type remoteScanItem struct {
	abs     string
	rel     string
	matcher gitzip.Matcher
}

type remoteScanResult struct {
	Paths                []string `json:"paths"`
	Files                int      `json:"files"`
	Directories          int      `json:"directories"`
	Symlinks             int      `json:"symlinks"`
	Bytes                int64    `json:"bytes"`
	Ignored              int      `json:"ignored"`
	ProtectedEnvExcluded int      `json:"protected_env_excluded"`
}

func scanRemote(ctx context.Context, conn *sshremote.Connection, sourceRoot, outputPath, manifestPath string, extra, includes []string, includeEnv bool) (remoteScanResult, error) {
	selector, err := gitzip.NewSelector(includes)
	if err != nil {
		return remoteScanResult{}, err
	}
	matcher := gitzip.NewMatcher(extra)
	for _, candidate := range []string{outputPath, manifestPath} {
		if rel := remoteRelative(sourceRoot, candidate); rel != "" && rel != "." && !strings.HasPrefix(rel, "../") {
			matcher = matcher.AddPattern("/"+rel, "")
		}
	}
	queue := []remoteScanItem{{abs: sourceRoot, rel: "", matcher: matcher}}
	result := remoteScanResult{}
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		cur := queue[0]
		queue = queue[1:]
		currentMatcher := cur.matcher
		for _, name := range gitzip.IgnoreFileNames {
			ignorePath := path.Join(cur.abs, name)
			content, _, _, readErr := conn.ReadFile(ignorePath, "utf8", 2<<20)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					continue
				}
				return result, fmt.Errorf("leer ignore remoto %s: %w", ignorePath, readErr)
			}
			currentMatcher = currentMatcher.AddContent(content, cur.rel)
		}
		entries, readErr := conn.List(cur.abs)
		if readErr != nil {
			return result, readErr
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, info := range entries {
			rel := strings.TrimPrefix(path.Join(cur.rel, info.Name), "./")
			abs := path.Join(cur.abs, info.Name)
			isDir := info.Type == "directory"
			ignored := currentMatcher.Ignored(rel, isDir)
			if !includeEnv && gitzip.IsProtectedEnv(rel) {
				ignored = true
				result.ProtectedEnvExcluded++
			}
			if ignored {
				result.Ignored++
				continue
			}
			selected := selector.Includes(rel, isDir)
			if info.Type == "symlink" {
				if !selected {
					result.Ignored++
					continue
				}
				result.Paths = append(result.Paths, rel)
				result.Symlinks++
			} else if isDir {
				queue = append(queue, remoteScanItem{abs: abs, rel: rel, matcher: currentMatcher})
				if !selected {
					continue
				}
				result.Paths = append(result.Paths, rel+"/")
				result.Directories++
			} else {
				if !selected {
					result.Ignored++
					continue
				}
				result.Paths = append(result.Paths, rel)
				result.Files++
				result.Bytes += info.Size
			}
		}
	}
	return result, nil
}

func remoteCreateGitZip(ctx context.Context, args map[string]any, env Env, source string, format gitzip.Format, extra, includes []string, includeEnv bool) (string, error) {
	manager := sshremote.GetManager(env.ConfigDir, env.RequestSecret, env.Confirm)
	conn, err := manager.Get(requiredArg(args, "connection_id"))
	if err != nil {
		return "", err
	}
	unlock := conn.LockOperation()
	defer unlock()
	if str(args, "format") == "" && str(args, "output_path") == "" {
		format = gitzip.FormatTARGZ
	}
	sourceRoot := conn.Resolve(source)
	output := strings.TrimSpace(str(args, "output_path"))
	if output == "" {
		name := path.Base(sourceRoot)
		if name == "." || name == "/" || name == "" {
			name = "project"
		}
		output = path.Join(sourceRoot, name+"."+gitzip.Extension(format))
	} else if path.IsAbs(output) {
		output = path.Clean(output)
	} else {
		output = path.Join(sourceRoot, output)
	}
	if _, statErr := conn.Stat(output); statErr == nil {
		if !boolArgOr(args, "overwrite", false) {
			return "", errors.New("el archivo remoto de salida ya existe; usa overwrite=true")
		}
		if err = conn.Delete(output, false); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("comprobar archivo remoto de salida: %w", statErr)
	}
	manifest := path.Join("/tmp", fmt.Sprintf("lilith-gitzip-%d.list", time.Now().UnixNano()))
	scan, err := scanRemote(ctx, conn, sourceRoot, output, manifest, extra, includes, includeEnv)
	if err != nil {
		return "", err
	}
	archiveArgs, err := validateArchiveArgs(format, stringSliceArg(args, "archive_args"))
	if err != nil {
		return "", err
	}
	var manifestData, command string
	if format == gitzip.FormatZIP {
		for _, entry := range scan.Paths {
			if strings.ContainsAny(entry, "\r\n") {
				return "", errors.New("ZIP remoto no puede empaquetar de forma segura rutas con saltos de línea; usa tar o tar.gz")
			}
		}
		manifestData = strings.Join(scan.Paths, "\n")
		if len(scan.Paths) > 0 {
			manifestData += "\n"
		}
		zipArgs := append([]string{}, archiveArgs...)
		if level := intArgOr(args, "compression_level", -1); level >= 0 && level <= 9 {
			zipArgs = append([]string{shellQuote(fmt.Sprintf("-%d", level))}, zipArgs...)
		}
		command = "mkdir -p -- " + shellQuote(path.Dir(output)) + " && cd -- " + shellQuote(sourceRoot) + " && zip " + strings.Join(zipArgs, " ") + " " + shellQuote(output) + " -@ < " + shellQuote(manifest)
	} else {
		manifestData = strings.Join(scan.Paths, "\x00")
		if len(scan.Paths) > 0 {
			manifestData += "\x00"
		}
		compression := ""
		prefix := ""
		if format == gitzip.FormatTARGZ {
			compression = "z"
			if level := intArgOr(args, "compression_level", -1); level >= 0 && level <= 9 {
				prefix = "GZIP=" + shellQuote(fmt.Sprintf("-%d", level)) + " "
			}
		}
		command = "mkdir -p -- " + shellQuote(path.Dir(output)) + " && cd -- " + shellQuote(sourceRoot) + " && " + prefix + "tar"
		if len(archiveArgs) > 0 {
			command += " " + strings.Join(archiveArgs, " ")
		}
		command += " --no-recursion --null -T " + shellQuote(manifest) + " -c" + compression + "f " + shellQuote(output)
	}
	if err = conn.WriteFile(manifest, manifestData, "utf8", true); err != nil {
		return "", err
	}
	defer conn.Delete(manifest, false)
	result, runErr := conn.Exec(ctx, command, secondsArg(args, "timeout_seconds", 600), false, 0, 0)
	if runErr != nil {
		text, _ := jsonOutput(result)
		return text, runErr
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(firstNonEmpty(result.Stderr, result.Stdout, fmt.Sprintf("exit %d", result.ExitCode)))
		return "", fmt.Errorf("comando remoto %s falló: %s", format, detail)
	}
	stats, statErr := conn.Stat(output)
	if statErr != nil {
		return "", statErr
	}
	return jsonOutput(map[string]any{
		"ok": true, "action": "remote_create", "connection_id": conn.ID, "format": format,
		"source_path": sourceRoot, "output_path": output, "archive_bytes": stats.Size,
		"source_bytes": scan.Bytes, "files": scan.Files, "directories": scan.Directories,
		"symlinks": scan.Symlinks, "ignored_entries": scan.Ignored,
		"protected_env_excluded": scan.ProtectedEnvExcluded, "command_result": result,
		"message": "Remote archive created from an explicit gitignore-aware manifest.",
	})
}

func remoteExtractGitZip(ctx context.Context, args map[string]any, env Env, source string, format gitzip.Format) (string, error) {
	manager := sshremote.GetManager(env.ConfigDir, env.RequestSecret, env.Confirm)
	conn, err := manager.Get(requiredArg(args, "connection_id"))
	if err != nil {
		return "", err
	}
	unlock := conn.LockOperation()
	defer unlock()
	archive := conn.Resolve(source)
	format = gitzip.InferFormat(str(args, "format"), archive, gitzip.FormatZIP)
	destination := strings.TrimSpace(str(args, "extract_path"))
	if destination == "" {
		destination = path.Dir(archive)
	} else {
		destination = conn.Resolve(destination)
	}
	result, err := extractRemote(ctx, conn, archive, destination, format, boolArgOr(args, "overwrite", false), secondsArg(args, "timeout_seconds", 600))
	if err != nil {
		return "", err
	}
	if boolArgOr(args, "cleanup_remote_archive", false) {
		if err = conn.Delete(archive, false); err != nil {
			return "", err
		}
	}
	return jsonOutput(map[string]any{
		"ok": true, "action": "remote_extract", "connection_id": conn.ID, "format": format,
		"archive_path": archive, "extract_path": destination,
		"archive_deleted": boolArgOr(args, "cleanup_remote_archive", false),
		"command_result":  result, "message": "Remote archive extracted successfully.",
	})
}

func extractRemote(ctx context.Context, conn *sshremote.Connection, archive, destination string, format gitzip.Format, overwrite bool, timeout time.Duration) (sshremote.ExecResult, error) {
	mkdir := "mkdir -p -- " + shellQuote(destination) + " && "
	var command string
	switch format {
	case gitzip.FormatZIP:
		flag := "-n"
		if overwrite {
			flag = "-o"
		}
		command = mkdir + "unzip " + flag + " " + shellQuote(archive) + " -d " + shellQuote(destination)
	case gitzip.FormatTARGZ:
		keep := ""
		if !overwrite {
			keep = "--keep-old-files "
		}
		command = mkdir + "tar " + keep + "-xzf " + shellQuote(archive) + " -C " + shellQuote(destination)
	case gitzip.FormatTAR:
		keep := ""
		if !overwrite {
			keep = "--keep-old-files "
		}
		command = mkdir + "tar " + keep + "-xf " + shellQuote(archive) + " -C " + shellQuote(destination)
	default:
		return sshremote.ExecResult{}, fmt.Errorf("formato remoto no compatible: %s", format)
	}
	result, err := conn.Exec(ctx, command, timeout, false, 0, 0)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(firstNonEmpty(result.Stderr, result.Stdout, fmt.Sprintf("exit %d", result.ExitCode)))
		return result, fmt.Errorf("extracción remota falló: %s", detail)
	}
	return result, nil
}

func validateArchiveArgs(format gitzip.Format, args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		valid := safeTarArg.MatchString(arg)
		if format == gitzip.FormatZIP {
			valid = safeZipArg.MatchString(arg)
		}
		if !valid {
			return nil, fmt.Errorf("archive_args contiene una opción no permitida: %s", arg)
		}
		out = append(out, shellQuote(arg))
	}
	if format == gitzip.FormatZIP && len(out) == 0 {
		out = append(out, "-q", "-y")
	}
	return out, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func remoteRelative(root, candidate string) string {
	root = strings.TrimSuffix(path.Clean(root), "/")
	candidate = path.Clean(candidate)
	if candidate == root {
		return "."
	}
	prefix := root + "/"
	if strings.HasPrefix(candidate, prefix) {
		return strings.TrimPrefix(candidate, prefix)
	}
	return "../"
}
func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch values := v.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		data, _ := json.Marshal(values)
		_ = data
		return nil
	}
}
