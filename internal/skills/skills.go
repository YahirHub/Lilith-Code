// Package skills carga "Claude Agent Skills" (SKILL.md con frontmatter YAML)
// desde el directorio de usuario (~/.li/skills) y del proyecto (./.li/skills)
// e inyecta un catálogo compacto en el system prompt para que el modelo pueda
// pedir la skill correcta con read_files. Está inspirado en la implementación
// de pi.dev (packages/coding-agent/src/core/skills.ts), adaptada a Go.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill es una instrucción reutilizable definida por un SKILL.md.
type Skill struct {
	Name        string // slug (kebab-case)
	Description string // one-liner (frontmatter: description)
	FilePath    string // ruta absoluta a SKILL.md
	BaseDir     string // dirname(FilePath)
	Source      string // "user" | "project" | "path"
}

const (
	maxName        = 64
	maxDescription = 1024
)

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	fmDelim   = "---"
	fmKeyLine = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*:\s*(.*)$`)
)

// LoadOptions controla de dónde se leen las skills.
type LoadOptions struct {
	UserDir    string // normalmente ~/.li/skills (se resuelve por caller)
	ProjectDir string // normalmente <cwd>/.li/skills
}

// Load descubre skills en las carpetas indicadas. Cada carpeta puede contener
// subdirectorios con un SKILL.md dentro. En caso de colisión por nombre gana
// la del proyecto (más cercana al usuario). Los errores individuales se
// silencian: una skill inválida no debe romper el arranque.
func Load(opts LoadOptions) []Skill {
	seen := map[string]Skill{}
	// Orden: primero user, luego project (project sobrescribe).
	scan(opts.UserDir, "user", seen)
	scan(opts.ProjectDir, "project", seen)

	out := make([]Skill, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// scan busca SKILL.md un nivel bajo dir (dir/<slug>/SKILL.md).
func scan(dir, source string, out map[string]Skill) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		file := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(file); err != nil {
			continue
		}
		s, ok := loadFile(file, source)
		if !ok {
			continue
		}
		out[s.Name] = s
	}
}

// loadFile parsea el frontmatter y valida los campos obligatorios.
func loadFile(path, source string) (Skill, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	fm := parseFrontmatter(string(data))
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		return Skill{}, false
	}
	if len(name) > maxName || !nameRe.MatchString(name) {
		return Skill{}, false
	}
	if len(desc) > maxDescription {
		desc = desc[:maxDescription]
	}
	return Skill{
		Name:        name,
		Description: desc,
		FilePath:    path,
		BaseDir:     filepath.Dir(path),
		Source:      source,
	}, true
}

// parseFrontmatter extrae los pares key:value del bloque YAML inicial ---.
// No es un parser YAML completo; sólo soporta strings de una línea, que es lo
// único que la spec de Skills necesita (name/description/disable-model-invocation).
func parseFrontmatter(content string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fmDelim {
		return out
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == fmDelim {
			return out
		}
		m := fmKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		val := strings.TrimSpace(m[2])
		val = strings.Trim(val, `"'`)
		out[strings.ToLower(m[1])] = val
	}
	return out
}

// ReadContent devuelve el cuerpo del SKILL.md sin frontmatter (o el archivo
// completo si no hay frontmatter). Se usa cuando el usuario invoca
// explícitamente /skills:<nombre>: la instrucción entera se inyecta en el
// turno para que el modelo la ejecute.
func ReadContent(s Skill) (string, error) {
	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		return "", err
	}
	text := string(data)
	if !strings.HasPrefix(strings.TrimSpace(text), fmDelim) {
		return text, nil
	}
	// Saltar el segundo delimitador.
	idx := strings.Index(text, fmDelim)
	if idx < 0 {
		return text, nil
	}
	rest := text[idx+len(fmDelim):]
	end := strings.Index(rest, fmDelim)
	if end < 0 {
		return text, nil
	}
	return strings.TrimSpace(rest[end+len(fmDelim):]), nil
}

// FormatForPrompt renderiza el bloque XML con las skills disponibles para
// que el modelo sepa que existen (y sepa dónde leerlas con read_files). El
// formato sigue el estándar de Agent Skills usado por pi/Claude Code.
func FormatForPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nThe following user-defined skills provide expert instructions for specific tasks. ")
	b.WriteString("When a task matches a skill's description, load its SKILL.md with read_files and follow it before acting. ")
	b.WriteString("Resolve any relative paths mentioned inside a skill against the skill's own directory (dirname of its path).\n")
	b.WriteString("\n<available_skills>\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "  <skill>\n    <name>%s</name>\n    <description>%s</description>\n    <path>%s</path>\n  </skill>\n",
			escapeXML(s.Name), escapeXML(s.Description), escapeXML(s.FilePath))
	}
	b.WriteString("</available_skills>\n")
	b.WriteString("\nExplicit invocation: the user may run `/skills:<name>` to force a skill; when this happens, follow the injected instructions immediately without asking for confirmation.\n")
	return b.String()
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// Find devuelve la skill por nombre exacto (case-insensitive).
func Find(list []Skill, name string) *Skill {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range list {
		if strings.ToLower(list[i].Name) == name {
			return &list[i]
		}
	}
	return nil
}

// Filter devuelve las skills cuyo nombre contiene `query` como subsecuencia,
// ignorando mayúsculas. Se usa para el autocompletado del prompt.
func Filter(list []Skill, query string) []Skill {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]Skill(nil), list...)
	}
	// Aceptar "skills:foo" o "foo" indistintamente.
	q = strings.TrimPrefix(q, "skills:")
	out := []Skill{}
	for _, s := range list {
		if strings.Contains(strings.ToLower(s.Name), q) {
			out = append(out, s)
		}
	}
	return out
}

// UserDir devuelve <configDir>/skills.
func UserDir(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, "skills")
}

// ProjectDir devuelve <projectRoot>/.li/skills.
func ProjectDir(projectRoot string) string {
	if strings.TrimSpace(projectRoot) == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".li", "skills")
}
