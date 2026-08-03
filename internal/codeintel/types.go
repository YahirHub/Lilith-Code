// Package codeintel provides language-aware repository detection, syntax
// indexing, semantic LSP queries, optional SCIP lookup and project validation.
// It is intentionally independent from the TUI so main agents and subagents can
// share the same concurrency-safe index.
package codeintel

import "time"

const indexVersion = 2

// Environment describes the host where Lilith is running.
type Environment struct {
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	Distribution string            `json:"distribution,omitempty"`
	Termux       bool              `json:"termux"`
	WSL          bool              `json:"wsl,omitempty"`
	SSH          bool              `json:"ssh,omitempty"`
	Container    bool              `json:"container,omitempty"`
	Shell        string            `json:"shell,omitempty"`
	Shells       []string          `json:"shells,omitempty"`
	Path         []string          `json:"path,omitempty"`
	Tools        map[string]string `json:"tools,omitempty"`
	PackageTools []string          `json:"package_tools,omitempty"`
}

// Project describes detected manifests, ecosystems and source languages.
type Project struct {
	Root            string         `json:"root"`
	Kinds           []string       `json:"kinds,omitempty"`
	Frameworks      []string       `json:"frameworks,omitempty"`
	Manifests       []string       `json:"manifests,omitempty"`
	Languages       map[string]int `json:"languages,omitempty"`
	PrimaryLanguage string         `json:"primary_language,omitempty"`
	PackageManager  string         `json:"package_manager,omitempty"`
	Monorepo        bool           `json:"monorepo,omitempty"`
}

// Profile is the inexpensive runtime/project snapshot injected into prompts.
type Profile struct {
	Environment Environment `json:"environment"`
	Project     Project     `json:"project"`
	Adapters    []string    `json:"adapters,omitempty"`
	LSPServers  []string    `json:"lsp_servers,omitempty"`
	SCIPIndex   string      `json:"scip_index,omitempty"`
}

// Symbol is a language-neutral declaration extracted from syntax.
type Symbol struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Package       string `json:"package,omitempty"`
	Container     string `json:"container,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Language      string `json:"language,omitempty"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	StartByte     uint32 `json:"start_byte,omitempty"`
	EndByte       uint32 `json:"end_byte,omitempty"`
	NodeType      string `json:"node_type,omitempty"`
}

// Reference is a syntax-level call/reference location.
type Reference struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Package       string `json:"package,omitempty"`
	Container     string `json:"container,omitempty"`
	Receiver      string `json:"receiver,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Language      string `json:"language,omitempty"`
	Path          string `json:"path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	StartByte     uint32 `json:"start_byte,omitempty"`
	EndByte       uint32 `json:"end_byte,omitempty"`
}

// FileRecord is the persistent, incremental representation of one source file.
type FileRecord struct {
	Path          string            `json:"path"`
	Language      string            `json:"language,omitempty"`
	Package       string            `json:"package,omitempty"`
	PackagePath   string            `json:"package_path,omitempty"`
	ImportAliases map[string]string `json:"import_aliases,omitempty"`
	Size          int64             `json:"size"`
	ModUnixNano   int64             `json:"mod_unix_nano"`
	SHA256        string            `json:"sha256,omitempty"`
	LineCount     int               `json:"line_count"`
	Backend       string            `json:"backend,omitempty"`
	Grammar       string            `json:"grammar,omitempty"`
	ParseError    string            `json:"parse_error,omitempty"`
	HasErrors     bool              `json:"has_errors,omitempty"`
	Symbols       []Symbol          `json:"symbols,omitempty"`
	References    []Reference       `json:"references,omitempty"`
	Imports       []string          `json:"imports,omitempty"`
}

// Index is serialized atomically outside the repository under ~/.li.
type Index struct {
	Version   int                   `json:"version"`
	Root      string                `json:"root"`
	UpdatedAt time.Time             `json:"updated_at"`
	Files     map[string]FileRecord `json:"files"`
}

// IndexStats summarizes refresh activity.
type IndexStats struct {
	Files      int       `json:"files"`
	Symbols    int       `json:"symbols"`
	References int       `json:"references"`
	Parsed     int       `json:"parsed"`
	Removed    int       `json:"removed"`
	Skipped    int       `json:"skipped"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ContextChunk is an AST-sized snippet selected for a model request.
type ContextChunk struct {
	Path      string `json:"path"`
	Language  string `json:"language,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Kind      string `json:"kind,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Score     int    `json:"score"`
	Content   string `json:"content"`
}

// ValidationOptions controls project-aware checks.
type ValidationOptions struct {
	ChangedPaths []string
	Mode         string // quick or full
	ApplyFormat  bool
	Timeout      time.Duration
}

// ValidationStep is one formatter/compiler/test invocation.
type ValidationStep struct {
	Name       string        `json:"name"`
	Command    string        `json:"command"`
	Dir        string        `json:"dir,omitempty"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration"`
	Skipped    bool          `json:"skipped,omitempty"`
	Reason     string        `json:"reason,omitempty"`
	Output     string        `json:"output,omitempty"`
	Successful bool          `json:"successful"`
}

// ValidationResult is returned even when one or more checks fail, allowing the
// coding agent to repair the project from structured output.
type ValidationResult struct {
	Profile    Profile          `json:"profile"`
	Steps      []ValidationStep `json:"steps"`
	Successful bool             `json:"successful"`
}

// SemanticRequest is an optional LSP query.
type SemanticRequest struct {
	Operation string
	Path      string
	Line      int
	Column    int
}

// SemanticResult contains raw protocol data plus the selected server.
type SemanticResult struct {
	Server string `json:"server"`
	Method string `json:"method"`
	Result any    `json:"result,omitempty"`
}

// GraphNode is a ranked file or symbol in the repository map.
type GraphNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
	Weight   int    `json:"weight"`
}

// GraphEdge is a dependency, call, definition or test relation.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// RepositoryGraph is a compact task-oriented repository map.
type RepositoryGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
