package gitzip

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipNames(t *testing.T, archive string) []string {
	t.Helper()
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := make([]string, 0, len(r.File))
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

func tarGZNames(t *testing.T, archive string) []string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, h.Name)
	}
	sort.Strings(out)
	return out
}

func TestCreateHonorsNestedIgnoresAndProtectsEnv(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".gitignore"), "dist/\n*.tmp\n")
	writeTestFile(t, filepath.Join(root, "keep.txt"), "ok")
	writeTestFile(t, filepath.Join(root, ".env"), "TOKEN=secret")
	writeTestFile(t, filepath.Join(root, ".env.example"), "TOKEN=")
	writeTestFile(t, filepath.Join(root, "dist", "bundle.js"), "ignored")
	writeTestFile(t, filepath.Join(root, "nested", ".lilithignore"), "private.txt\n")
	writeTestFile(t, filepath.Join(root, "nested", "private.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "nested", "public.txt"), "included")
	writeTestFile(t, filepath.Join(root, ".git", "config"), "ignored")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "bundle.zip")
	res, err := Create(context.Background(), Options{SourceRoot: root, OutputPath: archive, Format: FormatZIP})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProtectedEnvExcluded != 1 {
		t.Fatalf("protected env excluded=%d", res.ProtectedEnvExcluded)
	}
	names := strings.Join(zipNames(t, archive), "\n")
	for _, want := range []string{"keep.txt", ".env.example", "nested/public.txt", "empty/"} {
		if !strings.Contains(names, want) {
			t.Fatalf("falta %q en:\n%s", want, names)
		}
	}
	for _, forbidden := range []string{".env\n", "dist/bundle.js", "nested/private.txt", ".git/config", "bundle.zip"} {
		if strings.Contains(names, forbidden) {
			t.Fatalf("entrada prohibida %q en:\n%s", forbidden, names)
		}
	}
}

func TestCreateTarGZAndOverwritePolicy(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main")
	archive := filepath.Join(root, "release.tar.gz")
	if _, err := Create(context.Background(), Options{SourceRoot: root, OutputPath: archive, Format: FormatTARGZ}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tarGZNames(t, archive), "\n"); !strings.Contains(got, "main.go") || strings.Contains(got, "release.tar.gz") {
		t.Fatalf("manifest inesperado:\n%s", got)
	}
	if _, err := Create(context.Background(), Options{SourceRoot: root, OutputPath: archive, Format: FormatTARGZ}); err == nil {
		t.Fatal("se esperaba rechazo sin overwrite")
	}
	if _, err := Create(context.Background(), Options{SourceRoot: root, OutputPath: archive, Format: FormatTARGZ, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMatcherSupportsNegationAndLegacyIgnoreNames(t *testing.T) {
	m := NewMatcher(nil).AddContent("build/*\n!build/keep.txt\n", "")
	if !m.Ignored("build/drop.txt", false) {
		t.Fatal("build/drop.txt debía ignorarse")
	}
	if m.Ignored("build/keep.txt", false) {
		t.Fatal("la negación debía restaurar build/keep.txt")
	}
	for _, name := range []string{".gitignore", ".lilithignore", ".codewolfignore", ".codebuffignore", ".manicodeignore"} {
		found := false
		for _, got := range IgnoreFileNames {
			found = found || got == name
		}
		if !found {
			t.Fatalf("falta archivo ignore compatible %s", name)
		}
	}
}

func TestScanNeverDescendsIntoNestedGitMetadata(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "module", ".git", "objects", "secret"), "ignored")
	writeTestFile(t, filepath.Join(root, "module", "main.go"), "package module")

	entries, _, err := Scan(context.Background(), root, filepath.Join(root, "out.zip"), filepath.Join(root, "tmp"), nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(filepath.ToSlash(entry.Relative), "/.git/") || strings.HasSuffix(filepath.ToSlash(entry.Relative), "/.git") {
			t.Fatalf("se incluyó metadata Git anidada: %s", entry.Relative)
		}
	}
}

func TestSelectorDoubleStarMatchesDirectAndNestedFiles(t *testing.T) {
	selector, err := NewSelector([]string{"assets/**/*.png"})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"assets/logo.png", "assets/icons/logo.png", "assets/icons/dark/logo.png"} {
		if !selector.Includes(rel, false) {
			t.Fatalf("** no incluyó %s", rel)
		}
	}
	for _, rel := range []string{"assets/logo.svg", "docs/logo.png"} {
		if selector.Includes(rel, false) {
			t.Fatalf("** incluyó ruta no seleccionada %s", rel)
		}
	}
}

func TestSelectorRejectsPathsOutsideSource(t *testing.T) {
	if _, err := NewSelector([]string{"../secret.txt"}); err == nil {
		t.Fatal("include_paths debía rechazar rutas fuera de source_path")
	}
}

func TestCreateSupportsIncludeAndExcludePaths(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "app", "main.go"), "package main")
	writeTestFile(t, filepath.Join(root, "app", "debug.log"), "omit")
	writeTestFile(t, filepath.Join(root, "docs", "guide.md"), "omit by include")
	writeTestFile(t, filepath.Join(root, "assets", "logo.png"), "png")
	archive := filepath.Join(root, "selected.zip")
	_, err := Create(context.Background(), Options{
		SourceRoot:    root,
		OutputPath:    archive,
		Format:        FormatZIP,
		IncludePaths:  []string{"app/", "assets/logo.png"},
		ExtraExcludes: []string{"*.log"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Join(zipNames(t, archive), "\n")
	for _, want := range []string{"app/", "app/main.go", "assets/logo.png"} {
		if !strings.Contains(names, want) {
			t.Fatalf("falta %q en:\n%s", want, names)
		}
	}
	for _, forbidden := range []string{"app/debug.log", "docs/guide.md"} {
		if strings.Contains(names, forbidden) {
			t.Fatalf("se incluyó %q en:\n%s", forbidden, names)
		}
	}
}
