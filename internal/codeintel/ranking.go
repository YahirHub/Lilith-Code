package codeintel

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var semanticStopWords = map[string]bool{
	"a": true, "al": true, "algo": true, "and": true, "con": true, "cuando": true,
	"de": true, "del": true, "desde": true, "el": true, "en": true, "es": true,
	"esta": true, "este": true, "for": true, "from": true, "hacia": true, "hasta": true,
	"la": true, "las": true, "lo": true, "los": true, "of": true, "o": true, "para": true,
	"por": true, "que": true, "se": true, "sin": true, "su": true, "the": true, "to": true,
	"un": true, "una": true, "unos": true, "y": true,
}

var semanticSynonymGroups = [][]string{
	{"network", "red", "internet", "connectivity", "connection", "conexion", "offline", "online", "tcp", "dial", "socket"},
	{"retry", "retries", "reintentar", "reintento", "reintentos", "reconnect", "reconexion", "backoff"},
	{"wait", "waiting", "esperar", "espera", "delay", "sleep", "pause"},
	{"temporary", "temporales", "temporal", "transient", "recoverable", "recuperable"},
	{"error", "errors", "errores", "failure", "failures", "fallo", "fallos"},
	{"provider", "proveedor", "client", "cliente", "openai", "codex", "transport", "transporte"},
	{"response", "respuesta", "responses", "stream", "streaming", "chunk", "chunks", "sse"},
	{"partial", "parcial", "incomplete", "incompleta", "truncated", "truncado"},
	{"duplicate", "duplicar", "duplicado", "duplication", "dedupe", "deduplicate"},
	{"message", "mensaje", "prompt", "request", "solicitud", "turn", "turno"},
	{"send", "sending", "enviar", "envio", "dispatch", "submit"},
	{"call", "calls", "llamada", "llamadas", "invoke", "invocar"},
	{"symbol", "simbolo", "definition", "definicion", "declaration", "declaracion"},
	{"reference", "references", "referencia", "referencias", "usage", "usos"},
	{"test", "tests", "prueba", "pruebas", "testing"},
	{"format", "formatter", "formato", "formatear", "gofmt"},
	{"build", "compile", "compilar", "compilacion", "compiler"},
	{"documentation", "docs", "documentacion", "readme", "markdown"},
	{"release", "releases", "version", "versiones", "tag", "publish", "publicar"},
	{"install", "installer", "instalar", "instalador", "setup"},
	{"tui", "terminal", "input", "chat", "interfaz"},
	{"graph", "grafo", "dependency", "dependencies", "dependencia", "dependencias", "flow", "flujo"},
}

var semanticSynonyms = buildSemanticSynonyms()

func buildSemanticSynonyms() map[string][]string {
	out := map[string][]string{}
	for _, group := range semanticSynonymGroups {
		for _, value := range group {
			for _, other := range group {
				if other != value {
					out[value] = append(out[value], other)
				}
			}
		}
	}
	return out
}

func semanticTerms(query string) map[string]int {
	terms := map[string]int{}
	for _, token := range semanticTokens(query) {
		if semanticStopWords[token] || len(token) < 2 {
			continue
		}
		if terms[token] < 6 {
			terms[token] = 6
		}
		for _, synonym := range semanticSynonyms[token] {
			if terms[synonym] < 2 {
				terms[synonym] = 2
			}
		}
	}
	return terms
}

func semanticTokens(value string) []string {
	value = normalizeAccents(value)
	var words []string
	var current []rune
	var previous rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(current)))
		current = current[:0]
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			previous = 0
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			flush()
		}
		current = append(current, unicode.ToLower(r))
		previous = r
	}
	flush()
	return words
}

func normalizeAccents(value string) string {
	return strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ü", "U", "Ñ", "N",
	).Replace(value)
}

func semanticScore(terms map[string]int, values ...string) int {
	if len(terms) == 0 {
		return 0
	}
	tokens := map[string]bool{}
	joined := ""
	for _, value := range values {
		for _, token := range semanticTokens(value) {
			tokens[token] = true
			joined += " " + token
		}
	}
	score := 0
	for term, weight := range terms {
		switch {
		case tokens[term]:
			score += weight * 12
		case strings.Contains(joined, " "+term):
			score += weight * 6
		case len(term) >= 4 && strings.Contains(joined, term):
			score += weight * 3
		}
	}
	return score
}

func recordSemanticValues(record FileRecord) []string {
	values := []string{record.Path, record.Language, record.Package, record.PackagePath, strings.Join(record.Imports, " ")}
	for _, symbol := range record.Symbols {
		values = append(values, symbol.Name, symbol.QualifiedName, symbol.Kind, symbol.Container)
	}
	for _, ref := range record.References {
		values = append(values, ref.Name, ref.QualifiedName, ref.Kind, ref.Container, ref.Receiver)
	}
	return values
}

func taskAllowsPath(path string, terms map[string]int) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	ext := strings.ToLower(filepath.Ext(lower))
	documentationIntent := terms["documentation"] > 0 || terms["docs"] > 0 || terms["readme"] > 0 || terms["markdown"] > 0
	releaseIntent := terms["release"] > 0 || terms["install"] > 0 || terms["installer"] > 0
	if !documentationIntent && (ext == ".md" || ext == ".rst" || strings.HasPrefix(lower, "contexto/") || strings.Contains(lower, "/contexto/")) {
		return false
	}
	if !releaseIntent && (strings.HasPrefix(lower, ".github/") || strings.Contains(lower, "/.github/")) {
		return false
	}
	return true
}

func pathRankingAdjustment(path string, terms map[string]int) int {
	lower := strings.ToLower(filepath.ToSlash(path))
	ext := strings.ToLower(filepath.Ext(lower))
	documentationIntent := terms["documentation"] > 0 || terms["docs"] > 0 || terms["readme"] > 0 || terms["markdown"] > 0
	releaseIntent := terms["release"] > 0 || terms["install"] > 0 || terms["installer"] > 0
	adjustment := 0
	switch ext {
	case ".go", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".java", ".kt", ".cs", ".php", ".py", ".js", ".ts", ".tsx", ".jsx", ".rb", ".swift", ".dart", ".ex", ".exs", ".gd":
		adjustment += 24
	case ".md", ".txt", ".rst":
		if documentationIntent {
			adjustment += 15
		} else {
			adjustment -= 140
		}
	case ".yml", ".yaml", ".toml", ".json":
		adjustment -= 15
	}
	if strings.HasPrefix(lower, "contexto/") || strings.Contains(lower, "/contexto/") {
		if !documentationIntent {
			adjustment -= 180
		}
	}
	if strings.HasPrefix(lower, ".github/") || strings.Contains(lower, "/.github/") {
		if !releaseIntent {
			adjustment -= 90
		}
	}
	if isTestPath(lower) {
		if terms["test"] > 0 {
			adjustment += 18
		} else {
			adjustment -= 90
		}
	}
	return adjustment
}

func isTestPath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(lower, "_test.") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") || strings.Contains(lower, "/tests/") || strings.HasPrefix(lower, "tests/")
}

func sortedSemanticTerms(terms map[string]int) []string {
	out := make([]string, 0, len(terms))
	for term := range terms {
		out = append(out, term)
	}
	sort.Strings(out)
	return out
}
