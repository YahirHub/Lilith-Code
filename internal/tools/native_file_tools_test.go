package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileHandlesLargeUnicodeContentExactly(t *testing.T) {
	for _, size := range []int{8 << 10, 64 << 10, 1 << 20} {
		t.Run(itoa(size), func(t *testing.T) {
			root := t.TempDir()
			unit := "línea 🚀 con Unicode\r\n"
			content := strings.Repeat(unit, size/len(unit))
			content += strings.Repeat("x", size-len(content))
			out, err := Execute(context.Background(), "write_file", map[string]any{
				"path": "reporte.md", "content": content,
			}, Env{Root: root})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "reporte.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != content {
				t.Fatalf("content mismatch: got %d bytes want %d", len(data), len(content))
			}
			sum := sha256.Sum256(data)
			if !strings.Contains(out, "bytes_written: "+itoa(len(data))) || !strings.Contains(out, hex.EncodeToString(sum[:])) || !strings.Contains(out, "atomic: yes") {
				t.Fatalf("missing verification metadata:\n%s", out)
			}
			assertNoWriteTemps(t, root)
		})
	}
}

func TestAtomicCreateDoesNotClobberExistingDestination(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "race.txt")
	if err := os.WriteFile(path, []byte("other process"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := atomicCreateFile(context.Background(), path, []byte("lilith"), 0o644)
	if err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("atomic create should return fs.ErrExist, got %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "other process" {
		t.Fatalf("existing destination was overwritten: %q", data)
	}
	assertNoWriteTemps(t, root)
}

func TestWriteFileRequiresExplicitOverwrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	out, err := Execute(context.Background(), "write_file", map[string]any{
		"path": "report.md", "content": "new",
	}, Env{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "OVERWRITE_REQUIRED:") {
		t.Fatalf("unexpected result: %q", out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old" {
		t.Fatalf("existing file changed without overwrite=true: %q", data)
	}

	oldSum := sha256.Sum256(data)
	out, err = Execute(context.Background(), "write_file", map[string]any{
		"path":            "report.md",
		"overwrite":       true,
		"expected_sha256": hex.EncodeToString(oldSum[:]),
		"content":         "new\n",
	}, Env{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "replaced report.md") {
		t.Fatalf("unexpected overwrite result: %q", out)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "new\n" {
		t.Fatalf("overwrite failed: %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("mode not preserved: %o", info.Mode().Perm())
		}
	}
	assertNoWriteTemps(t, root)
}

func TestWriteFileRejectsStaleExpectedSHA(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "write_file", map[string]any{
		"path": "report.md", "overwrite": true,
		"expected_sha256": strings.Repeat("0", 64),
		"content":         "replacement",
	}, Env{Root: root})
	if err == nil || !strings.Contains(err.Error(), "FILE_CHANGED") {
		t.Fatalf("expected stale checksum error, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "current" {
		t.Fatalf("stale write mutated target: %q", data)
	}
}

func TestAppendFileBuildsReportAtomically(t *testing.T) {
	root := t.TempDir()
	expectedSHA := ""
	for i, section := range []string{"# Report\n\n", "## One\nαβγ\n", "## Two\nfinal\n"} {
		args := map[string]any{"path": "report.md", "content": section}
		if expectedSHA != "" {
			args["expected_sha256"] = expectedSHA
		}
		out, err := Execute(context.Background(), "append_file", args, Env{Root: root})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "atomic: yes") || !strings.Contains(out, "bytes_appended:") {
			t.Fatalf("append %d missing metadata: %q", i, out)
		}
		expectedSHA = outputField(out, "sha256")
		if len(expectedSHA) != 64 {
			t.Fatalf("append %d returned invalid sha256: %q", i, expectedSHA)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Report\n\n## One\nαβγ\n## Two\nfinal\n" {
		t.Fatalf("unexpected report: %q", data)
	}
	assertNoWriteTemps(t, root)
}

func TestAppendFileRejectsStaleExpectedSHA(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), "append_file", map[string]any{
		"path": "report.md", "expected_sha256": strings.Repeat("f", 64), "content": "second\n",
	}, Env{Root: root})
	if err == nil || !strings.Contains(err.Error(), "FILE_CHANGED") {
		t.Fatalf("expected stale append checksum error, got %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "first\n" {
		t.Fatalf("stale append changed destination: %q", data)
	}
}

func TestNativeWriterRejectsPayloadAboveOneMiBWithoutCreatingFile(t *testing.T) {
	root := t.TempDir()
	_, err := Execute(context.Background(), "write_file", map[string]any{
		"path": "too-large.md", "content": strings.Repeat("x", MaxNativeWriteBytes+1),
	}, Env{Root: root})
	if err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("expected bounded payload error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "too-large.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized payload created a file: %v", statErr)
	}
}

func TestCanceledWriteLeavesExistingFileUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Execute(ctx, "write_file", map[string]any{
		"path": "a.txt", "overwrite": true, "content": strings.Repeat("x", 1<<20),
	}, Env{Root: root})
	if err == nil || err != context.Canceled {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "before" {
		t.Fatalf("canceled write changed destination: %q", data)
	}
	assertNoWriteTemps(t, root)
}

func TestNativeWriterSchemasAdvertisePathBeforeContent(t *testing.T) {
	for _, name := range []string{"write_file", "append_file"} {
		schemas := Schemas([]string{name})
		if len(schemas) != 1 {
			t.Fatalf("missing schema for %s", name)
		}
		data, err := json.Marshal(schemas[0].Function.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Index(text, `"path"`) >= strings.Index(text, `"content"`) {
			t.Fatalf("%s schema should stream path before content: %s", name, text)
		}
	}
}

func outputField(output, name string) string {
	prefix := name + ":"
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func assertNoWriteTemps(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lilith-write-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}

func TestNativeFileToolSchemasExplainLimitsAndNewlines(t *testing.T) {
	writeDef, ok := Get("write_file")
	if !ok {
		t.Fatal("write_file not registered")
	}
	appendDef, ok := Get("append_file")
	if !ok {
		t.Fatal("append_file not registered")
	}
	for _, value := range []string{"1,048,576", "1 MiB", "append_file"} {
		if !strings.Contains(writeDef.Description, value) {
			t.Fatalf("write_file description missing %q: %s", value, writeDef.Description)
		}
	}
	for _, value := range []string{"1,048,576", "67,108,864", "no newline"} {
		if !strings.Contains(appendDef.Description, value) {
			t.Fatalf("append_file description missing %q: %s", value, appendDef.Description)
		}
	}
}
