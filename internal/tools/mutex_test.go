package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMutationKeyCanonicalizesSymlinkAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	alias := filepath.Join(root, "alias.txt")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if got, want := mutationKey(alias), mutationKey(real); got != want {
		t.Fatalf("symlink aliases must share a mutation queue: got=%q want=%q", got, want)
	}
	if lockFile(alias) != lockFile(real) {
		t.Fatal("symlink aliases should resolve to the same mutex")
	}
}
