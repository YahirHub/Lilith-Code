package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchRanksStrongestMatchesAndFindsBinaryAssetsByPath(t *testing.T) {
	t.Parallel()
	sk := makeSearchFixture(t)

	result, err := Search(sk, SearchOptions{Query: "dark mode", Limit: 6, ContextLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) < 2 {
		t.Fatalf("expected multiple hits, got %#v", result.Hits)
	}
	if result.Hits[0].Path != "references/dark-mode.md" {
		t.Fatalf("expected strongest exact content match first, got %#v", result.Hits[0])
	}
	foundAsset := false
	for _, hit := range result.Hits {
		if hit.Path == "assets/images/dark-mode-dashboard.png" {
			foundAsset = true
			if hit.Line != 0 || hit.Snippet != "" {
				t.Fatalf("binary asset should match by path only: %#v", hit)
			}
		}
	}
	if !foundAsset {
		t.Fatalf("expected binary asset filename/path match in %#v", result.Hits)
	}
}

func TestSearchFiltersKindsExtensionsRegexAndMaintenance(t *testing.T) {
	t.Parallel()
	sk := makeSearchFixture(t)

	res, err := Search(sk, SearchOptions{
		Query:        `data-bs-theme`,
		Regex:        true,
		Kinds:        []ResourceKind{KindAsset},
		Extensions:   []string{"html"},
		Pattern:      "assets/**/*.html",
		Limit:        5,
		ContextLines: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Path != "assets/snippets/dark-mode-toggle.html" {
		t.Fatalf("unexpected filtered search hits: %#v", res.Hits)
	}
	if strings.Contains(res.Hits[0].Snippet, "\n") {
		t.Fatalf("context_lines=0 should return only one line, got %q", res.Hits[0].Snippet)
	}

	// Maintainer memory can contain many duplicated keywords but must stay out of
	// normal runtime skill search unless explicitly requested.
	res, err = Search(sk, SearchOptions{Query: "maintainer-only-sentinel", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("maintainer docs leaked into normal search: %#v", res.Hits)
	}
	res, err = Search(sk, SearchOptions{Query: "maintainer-only-sentinel", Limit: 5, IncludeMaintenance: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Path != "AGENTS.md" {
		t.Fatalf("expected explicit maintainer search hit, got %#v", res.Hits)
	}
}

func TestListFilesAndReadFileAreBoundedAndSafe(t *testing.T) {
	t.Parallel()
	sk := makeSearchFixture(t)

	files, truncated, err := ListFiles(sk, ListFilesOptions{Kinds: []ResourceKind{KindScript}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(files) != 1 || files[0].Path != "scripts/search_skill.py" || !files[0].Text {
		t.Fatalf("unexpected script listing: truncated=%v files=%#v", truncated, files)
	}

	read, err := ReadFile(sk, "references/dark-mode.md", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if read.Offset != 2 || read.EndLine != 3 || read.Total < 4 || !read.Truncated {
		t.Fatalf("unexpected bounded read metadata: %#v", read)
	}
	if strings.Count(read.Content, "\n") != 1 {
		t.Fatalf("expected exactly two lines, got %q", read.Content)
	}

	binary, err := ReadFile(sk, "assets/images/dark-mode-dashboard.png", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !binary.Binary || binary.Content != "" {
		t.Fatalf("binary file should not be inlined: %#v", binary)
	}

	if _, err := ReadFile(sk, "../outside.txt", 1, 20); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}

	outside := filepath.Join(filepath.Dir(sk.BaseDir), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("secret outside skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sk.BaseDir, "references", "outside-link.txt")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := ReadFile(sk, "references/outside-link.txt", 1, 20); err == nil {
			t.Fatal("expected symlink escaping skill root to be rejected")
		}
	}
}

func makeSearchFixture(t *testing.T) Skill {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", "---\nname: bootstrap-ui-design\ndescription: Bootstrap UI guidance.\n---\nUse references and assets selectively.\n")
	write("references/dark-mode.md", "# Dark mode\nUse dark mode with Bootstrap 5.3.\nSet data-bs-theme=dark on the root element.\nValidate contrast afterwards.\n")
	write("references/forms.md", "# Forms\nDark inputs should still have visible labels.\n")
	write("scripts/search_skill.py", "def search_skill(query):\n    return query\n")
	write("assets/snippets/dark-mode-toggle.html", "<html>\n<body data-bs-theme=\"dark\">Toggle</body>\n</html>\n")
	write("AGENTS.md", "maintainer-only-sentinel dark mode memory\n")
	image := filepath.Join(root, "assets", "images", "dark-mode-dashboard.png")
	if err := os.MkdirAll(filepath.Dir(image), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	return Skill{Name: "bootstrap-ui-design", Description: "Bootstrap UI guidance.", FilePath: filepath.Join(root, "SKILL.md"), BaseDir: root, Source: "project"}
}
