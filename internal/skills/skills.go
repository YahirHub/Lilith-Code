// Package skills carga Agent Skills (SKILL.md con frontmatter YAML), descubre
// recursos asociados y ofrece navegación/búsqueda acotada para que el modelo no
// tenga que meter archivos grandes completos en contexto. Está inspirado en la
// implementación de pi.dev y adaptado a Go/Lilith.
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
	UserDir     string   // compatibilidad: ~/.li/skills
	ProjectDir  string   // compatibilidad: <cwd>/.li/skills
	UserDirs    []string // rutas adicionales de usuario, en orden de precedencia
	ProjectDirs []string // rutas adicionales de proyecto, en orden de precedencia
}

// Load descubre skills en las carpetas indicadas. Cada carpeta puede contener
// subdirectorios con un SKILL.md dentro. En caso de colisión por nombre gana
// la del proyecto (más cercana al usuario). Los errores individuales se
// silencian: una skill inválida no debe romper el arranque.
func Load(opts LoadOptions) []Skill {
	seen := map[string]Skill{}
	// Rutas más específicas se procesan después y por tanto sobrescriben
	// colisiones. Las skills de proyecto siguen ganando a las de usuario.
	userDirs := append([]string(nil), opts.UserDirs...)
	if strings.TrimSpace(opts.UserDir) != "" {
		userDirs = append(userDirs, opts.UserDir)
	}
	projectDirs := append([]string(nil), opts.ProjectDirs...)
	if strings.TrimSpace(opts.ProjectDir) != "" {
		projectDirs = append(projectDirs, opts.ProjectDir)
	}
	for _, dir := range userDirs {
		scan(dir, "user", seen)
	}
	for _, dir := range projectDirs {
		scan(dir, "project", seen)
	}

	out := make([]Skill, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DefaultLoadOptions añade las ubicaciones nativas de Lilith y las rutas de
// Claude/Agent Skills que el usuario ya utiliza. No necesita configuración
// adicional y conserva la precedencia proyecto > usuario y .li > compatibilidad.
func DefaultLoadOptions(configDir, projectRoot string) LoadOptions {
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	var userDirs []string
	if home != "" {
		userDirs = append(userDirs,
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".claude", "skills"),
		)
	}
	var projectDirs []string
	if strings.TrimSpace(projectRoot) != "" {
		projectDirs = append(projectDirs,
			filepath.Join(projectRoot, ".agents", "skills"),
			filepath.Join(projectRoot, ".claude", "skills"),
		)
	}
	return LoadOptions{
		UserDir:     UserDir(configDir),
		ProjectDir:  ProjectDir(projectRoot),
		UserDirs:    userDirs,
		ProjectDirs: projectDirs,
	}
}

// scan descubre SKILL.md de forma recursiva. Cuando un directorio ya contiene
// SKILL.md se trata como raíz de skill y no se entra en sus assets/scripts para
// evitar detectar accidentalmente recursos internos como otras skills.
func scan(dir, source string, out map[string]Skill) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
		}
		file := filepath.Join(path, "SKILL.md")
		if info, statErr := os.Stat(file); statErr == nil && !info.IsDir() {
			if s, ok := loadFile(file, source); ok {
				out[s.Name] = s
			}
			if path != root {
				return filepath.SkipDir
			}
		}
		return nil
	})
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

// parseFrontmatter extrae los valores simples que Lilith necesita. Además de
// `key: value`, soporta escalares YAML plegados/literales (`>` y `|`), usados
// habitualmente para descriptions largas en Agent Skills. No pretende ser un
// parser YAML general.
func parseFrontmatter(content string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fmDelim {
		return out
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == fmDelim {
			return out
		}
		m := fmKeyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(m[2])
		if val == ">" || val == ">-" || val == ">+" || val == "|" || val == "|-" || val == "|+" {
			folded := strings.HasPrefix(val, ">")
			var block []string
			for i+1 < len(lines) {
				next := lines[i+1]
				if strings.TrimSpace(next) == fmDelim {
					break
				}
				if strings.TrimSpace(next) != "" && len(next) > 0 && next[0] != ' ' && next[0] != '\t' {
					break
				}
				i++
				block = append(block, strings.TrimSpace(next))
			}
			if folded {
				val = strings.Join(block, " ")
			} else {
				val = strings.Join(block, "\n")
			}
		} else {
			val = strings.Trim(val, `"'`)
		}
		out[key] = strings.TrimSpace(val)
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

// FormatForPrompt renderiza el bloque XML de Agent Skills siguiendo el patrón
// de pi.dev: el catálogo sólo expone metadata y la SKILL.md se carga bajo demanda.
// Lilith refuerza la instrucción para que una skill aplicable sea obligatoria,
// no una sugerencia que el modelo pueda ignorar.
func FormatForPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.\n")
	b.WriteString("Before taking any substantive action, compare the user's task with the descriptions in <available_skills>.\n")
	b.WriteString("If the task clearly matches a skill description, you MUST load that skill's SKILL.md with skill_read before inspecting the project, editing files, running commands, or answering the task. Skills are mandatory when applicable, not optional hints. If skill_read reports that more of SKILL.md is available, continue reading it until the complete SKILL.md has been loaded before acting.\n")
	b.WriteString("Do not rely on the description alone and do not claim to follow a skill that you have not loaded. If multiple skills clearly apply, load each one that is needed. Do not load unrelated skills.\n")
	b.WriteString("After loading SKILL.md, follow its instructions as task-specific guidance. For large secondary references, scripts, examples, or assets, use skill_search, skill_files, and bounded skill_read calls instead of loading entire resources wholesale.\n")
	b.WriteString("When a skill references a relative path, resolve it against the skill directory (the parent of SKILL.md).\n")
	b.WriteString("\n<available_skills>\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "  <skill>\n    <name>%s</name>\n    <description>%s</description>\n    <location>%s</location>\n  </skill>\n",
			escapeXML(s.Name), escapeXML(s.Description), escapeXML(s.FilePath))
	}
	b.WriteString("</available_skills>\n")
	b.WriteString("\nExplicit invocation with `/skill:<name>` or `/skills:<name>` forces that skill and must be followed immediately.\n")
	return b.String()
}

// FormatInvocation genera la inyección de una skill explícita con el mismo
// contrato estructural que usa pi.dev: cuerpo completo, ubicación absoluta y
// base clara para resolver references/assets/scripts relativos.
func FormatInvocation(s Skill, content, additionalInstructions string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<skill name=\"%s\" location=\"%s\">\n", escapeXML(s.Name), escapeXML(s.FilePath))
	fmt.Fprintf(&b, "References are relative to %s.\n\n", filepath.ToSlash(s.BaseDir))
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n</skill>")
	if extra := strings.TrimSpace(additionalInstructions); extra != "" {
		b.WriteString("\n\n")
		b.WriteString(extra)
	}
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
