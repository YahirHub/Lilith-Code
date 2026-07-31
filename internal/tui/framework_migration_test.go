package tui

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTViewMigrationHasNoCharmbraceletDependency(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no se pudo resolver la ubicación de la prueba")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))

	for _, name := range []string{"go.mod", "go.sum"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("leer %s: %v", name, err)
		}
		if strings.Contains(string(content), "github.com/charmbracelet/") {
			t.Fatalf("%s todavía contiene una dependencia Charmbracelet", name)
		}
	}

	files := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "original" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			pathValue := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(pathValue, "github.com/charmbracelet/") {
				t.Errorf("import Charmbracelet restante en %s: %s", path, pathValue)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("auditar imports: %v", err)
	}
}
