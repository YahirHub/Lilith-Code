// Package skills carga Agent Skills (SKILL.md con frontmatter YAML), descubre
// recursos asociados y ofrece navegación/búsqueda acotada para que el modelo no
// tenga que meter archivos grandes completos en contexto. Está inspirado en la
// implementación de pi.dev y adaptado a Go/Lilith.
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Skill es una instrucción reutilizable definida por un SKILL.md.
type Skill struct {
	Name        string // slug (kebab-case)
	Description string // one-liner (frontmatter: description)
	FilePath    string // ruta absoluta a SKILL.md/legacy command
	BaseDir     string // dirname(FilePath)
	PluginRoot  string // non-empty for Claude plugin components
	Source      string // "builtin" | "user" | "project" | "path"

	// Claude-compatible frontmatter. Legacy .claude/commands are represented as
	// user-only skills so slash invocation can share the same runtime.
	DisableModelInvocation bool
	UserInvocable          bool
	AllowedTools           []string
	DisallowedTools        []string
	Model                  string
	Effort                 string
	Context                string
	Agent                  string
	Background             bool
	BackgroundSet          bool
	ArgumentHint           string
	Arguments              []string
	WhenToUse              string
	Paths                  []string
	Shell                  string
	HooksRaw               string
	LegacyCommand          bool
	Visibility             string // Claude skillOverrides: on|name-only|user-invocable-only|off
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
	BuiltinDir  string   // assets/skills embebidas, materializadas en caché
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
	// colisiones. Las embebidas son siempre el fallback de menor precedencia;
	// ~/.li/skills y las skills del proyecto pueden reemplazarlas por nombre.
	if strings.TrimSpace(opts.BuiltinDir) != "" {
		scan(opts.BuiltinDir, "builtin", seen)
	}
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

// DefaultLoadOptions añade primero las skills embebidas de assets/skills y
// después las ubicaciones nativas de Lilith y rutas Claude/Agent del usuario.
// No necesita configuración adicional y conserva la precedencia:
// proyecto > usuario > builtin, con .li > rutas compatibles dentro de cada ámbito.
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
		BuiltinDir:  BundledDir(configDir),
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
			// A directory with a Claude plugin manifest is a namespace boundary.
			// The plugin loader imports its components as plugin:name; the normal
			// recursive skill scan must not also expose them unscoped.
			if info, statErr := os.Stat(filepath.Join(path, ".claude-plugin", "plugin.json")); statErr == nil && !info.IsDir() {
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
	meta, body := parseSkillFrontmatter(string(data))
	name := strings.TrimSpace(meta.scalars["name"])
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	desc := strings.TrimSpace(meta.scalars["description"])
	if desc == "" {
		desc = firstParagraph(body)
	}
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
		Name: name, Description: desc, FilePath: path, BaseDir: filepath.Dir(path), Source: source,
		DisableModelInvocation: parseSkillBool(meta.scalars["disable-model-invocation"]),
		UserInvocable:          !strings.EqualFold(strings.TrimSpace(meta.scalars["user-invocable"]), "false"),
		AllowedTools:           splitSkillList(meta, "allowed-tools"),
		DisallowedTools:        splitSkillList(meta, "disallowed-tools"),
		Model:                  strings.TrimSpace(meta.scalars["model"]),
		Effort:                 strings.ToLower(strings.TrimSpace(meta.scalars["effort"])),
		Context:                strings.ToLower(strings.TrimSpace(meta.scalars["context"])),
		Agent:                  strings.TrimSpace(meta.scalars["agent"]),
		Background:             parseSkillBool(meta.scalars["background"]),
		BackgroundSet:          strings.TrimSpace(meta.scalars["background"]) != "",
		ArgumentHint:           strings.TrimSpace(meta.scalars["argument-hint"]),
		Arguments:              splitSkillList(meta, "arguments"),
		WhenToUse:              strings.TrimSpace(meta.scalars["when_to_use"]),
		Paths:                  splitSkillList(meta, "paths"),
		Shell:                  strings.ToLower(strings.TrimSpace(meta.scalars["shell"])),
		HooksRaw:               strings.TrimSpace(meta.blocks["hooks"]),
	}, true
}

type skillFrontmatter struct {
	scalars map[string]string
	lists   map[string][]string
	blocks  map[string]string
}

func parseSkillFrontmatter(content string) (skillFrontmatter, string) {
	fm := skillFrontmatter{scalars: map[string]string{}, lists: map[string][]string{}, blocks: map[string]string{}}
	text := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fmDelim {
		return fm, text
	}
	end := -1
	current := ""
	for i := 1; i < len(lines); i++ {
		raw := lines[i]
		trim := strings.TrimSpace(raw)
		if trim == fmDelim {
			end = i
			break
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "-") && current != "" {
			v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "-")), `"'`)
			if v != "" {
				fm.lists[current] = append(fm.lists[current], v)
			}
			continue
		}
		// Preserve nested hooks YAML so the hooks package can parse it using the
		// same lifecycle runner as settings.json. Other nested maps stay opaque.
		if len(raw)-len(strings.TrimLeft(raw, " \t")) == 0 {
			if key, val, ok := skillSplitKV(trim); ok && strings.EqualFold(key, "hooks") && strings.TrimSpace(val) == "" {
				var block []string
				for i+1 < len(lines) {
					next := lines[i+1]
					if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " \t")) == 0 {
						break
					}
					i++
					block = append(block, next)
				}
				fm.blocks["hooks"] = strings.Join(block, "\n")
				current = ""
				continue
			}
		}
		m := fmKeyLine.FindStringSubmatch(raw)
		if m == nil {
			current = ""
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		if key == "when-to-use" {
			key = "when_to_use"
		}
		val := strings.TrimSpace(m[2])
		current = ""
		if val == "" {
			current = key
			continue
		}
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			for _, item := range strings.Split(strings.Trim(val, "[]"), ",") {
				item = strings.Trim(strings.TrimSpace(item), `"'`)
				if item != "" {
					fm.lists[key] = append(fm.lists[key], item)
				}
			}
			continue
		}
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
		}
		fm.scalars[key] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	if end < 0 {
		return fm, text
	}
	return fm, strings.Join(lines[end+1:], "\n")
}

func splitSkillList(fm skillFrontmatter, key string) []string {
	if list := fm.lists[key]; len(list) > 0 {
		return append([]string(nil), list...)
	}
	raw := strings.TrimSpace(fm.scalars[key])
	if raw == "" {
		return nil
	}
	if key == "allowed-tools" || key == "disallowed-tools" || key == "arguments" || key == "paths" {
		return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	}
	return []string{raw}
}

func skillSplitKV(line string) (string, string, bool) {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func parseSkillBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func firstParagraph(body string) string {
	for _, part := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		p := strings.TrimSpace(part)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		p = strings.Join(strings.Fields(p), " ")
		if len(p) > maxDescription {
			p = p[:maxDescription]
		}
		return p
	}
	return ""
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

// ApplyClaudeOverrides applies Claude Code's skillOverrides from user, project
// and local settings. Project settings are considered only after workspace
// trust. Later scopes override earlier ones.
func ApplyClaudeOverrides(configDir, projectRoot string, trusted bool, in []Skill) []Skill {
	overrides := map[string]string{}
	home := filepath.Dir(filepath.Clean(configDir))
	paths := []string{}
	if strings.TrimSpace(configDir) != "" {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if trusted && strings.TrimSpace(projectRoot) != "" {
		paths = append(paths, filepath.Join(projectRoot, ".claude", "settings.json"), filepath.Join(projectRoot, ".claude", "settings.local.json"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			SkillOverrides map[string]string `json:"skillOverrides"`
		}
		if json.Unmarshal(data, &raw) != nil {
			continue
		}
		for name, value := range raw.SkillOverrides {
			v := strings.ToLower(strings.TrimSpace(value))
			if v == "on" || v == "name-only" || v == "user-invocable-only" || v == "off" {
				overrides[strings.ToLower(strings.TrimSpace(name))] = v
			}
		}
	}
	out := make([]Skill, 0, len(in))
	for _, sk := range in {
		v := overrides[strings.ToLower(sk.Name)]
		sk.Visibility = v
		switch v {
		case "off":
			continue
		case "user-invocable-only":
			sk.DisableModelInvocation = true
			sk.UserInvocable = true
		}
		out = append(out, sk)
	}
	return out
}

// FormatForPrompt renderiza el bloque XML de Agent Skills siguiendo el patrón
// de pi.dev: el catálogo sólo expone metadata y la SKILL.md se carga bajo demanda.
// Lilith refuerza la instrucción para que una skill aplicable sea obligatoria,
// no una sugerencia que el modelo pueda ignorar.
func FormatForPrompt(skills []Skill) string {
	visible := make([]Skill, 0, len(skills))
	for _, s := range skills {
		if !s.DisableModelInvocation {
			visible = append(visible, s)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	skills = visible
	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.\n")
	b.WriteString("Before taking any substantive action, compare the user's task with the descriptions in <available_skills>.\n")
	b.WriteString("If the task clearly matches a skill description, you MUST load that skill's SKILL.md with skill_read before inspecting the project, editing files, running commands, or answering the task. Skills are mandatory when applicable, not optional hints. If skill_read reports that more of SKILL.md is available, continue reading it until the complete SKILL.md has been loaded before acting.\n")
	b.WriteString("Do not rely on the description alone and do not claim to follow a skill that you have not loaded. If multiple skills clearly apply, load each one that is needed. Do not load unrelated skills.\n")
	b.WriteString("After loading SKILL.md, follow its instructions as task-specific guidance. For large secondary references, scripts, examples, or assets, use skill_search, skill_files, and bounded skill_read calls instead of loading entire resources wholesale.\n")
	b.WriteString("When a skill references a relative path, resolve it against the skill directory (the parent of SKILL.md).\n")
	b.WriteString("\n<available_skills>\n")
	for _, s := range skills {
		desc := strings.TrimSpace(s.Description)
		if s.Visibility == "name-only" {
			desc = ""
		} else if extra := strings.TrimSpace(s.WhenToUse); extra != "" {
			desc = strings.TrimSpace(desc + " " + extra)
		}
		if len(desc) > 1536 {
			desc = desc[:1536]
		}
		fmt.Fprintf(&b, "  <skill>\n    <name>%s</name>\n    <description>%s</description>\n    <location>%s</location>\n  </skill>\n",
			escapeXML(s.Name), escapeXML(desc), escapeXML(s.FilePath))
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

// LoadLegacyCommands imports Claude Code's legacy .claude/commands/*.md as
// explicit user-invocable skills. They are intentionally hidden from automatic
// model invocation, matching Claude's legacy slash-command semantics.
func LoadLegacyCommands(configDir, projectRoot string) []Skill {
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	seen := map[string]Skill{}
	if home != "" {
		scanCommands(filepath.Join(home, ".claude", "commands"), "user", seen)
	}
	if strings.TrimSpace(projectRoot) != "" {
		scanCommands(filepath.Join(projectRoot, ".claude", "commands"), "project", seen)
	}
	out := make([]Skill, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LoadLegacyCommandDirs loads explicit command directories for a Claude
// plugin. Callers apply the plugin namespace after parsing so the underlying
// legacy command format remains unchanged.
func LoadLegacyCommandDirs(dirs []string, source string) []Skill {
	seen := map[string]Skill{}
	for _, dir := range dirs {
		scanCommands(dir, source, seen)
	}
	out := make([]Skill, 0, len(seen))
	for _, skill := range seen {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func scanCommands(dir, source string, out map[string]Skill) {
	root := filepath.Clean(dir)
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fm, body := parseSkillFrontmatter(string(data))
		rel, _ := filepath.Rel(root, path)
		name := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
		name = strings.ReplaceAll(name, "/", ":")
		name = strings.ToLower(strings.TrimSpace(fm.scalars["name"]))
		if name == "" {
			rel, _ = filepath.Rel(root, path)
			name = strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
			name = strings.ReplaceAll(name, "/", "-")
		}
		if !nameRe.MatchString(name) {
			return nil
		}
		desc := strings.TrimSpace(fm.scalars["description"])
		if desc == "" {
			desc = firstParagraph(body)
		}
		if desc == "" {
			desc = "Claude legacy command /" + name
		}
		out[name] = Skill{
			Name: name, Description: desc, FilePath: path, BaseDir: filepath.Dir(path), Source: source,
			DisableModelInvocation: true, UserInvocable: true, AllowedTools: splitSkillList(fm, "allowed-tools"),
			DisallowedTools: splitSkillList(fm, "disallowed-tools"), Model: strings.TrimSpace(fm.scalars["model"]),
			Effort: strings.ToLower(strings.TrimSpace(fm.scalars["effort"])), Context: strings.ToLower(strings.TrimSpace(fm.scalars["context"])),
			Agent: strings.TrimSpace(fm.scalars["agent"]), Background: parseSkillBool(fm.scalars["background"]), BackgroundSet: strings.TrimSpace(fm.scalars["background"]) != "",
			ArgumentHint: strings.TrimSpace(fm.scalars["argument-hint"]), Arguments: splitSkillList(fm, "arguments"),
			WhenToUse: strings.TrimSpace(fm.scalars["when_to_use"]), Paths: splitSkillList(fm, "paths"), Shell: strings.ToLower(strings.TrimSpace(fm.scalars["shell"])), HooksRaw: strings.TrimSpace(fm.blocks["hooks"]), LegacyCommand: true,
		}
		return nil
	})
}

// ExpandArguments applies Claude-compatible $ARGUMENTS, $1..$9 and named
// positional substitutions before a skill/command is injected.
func ExpandArguments(s Skill, content, raw string) string {
	args := strings.Fields(raw)
	content = strings.ReplaceAll(content, "$ARGUMENTS", raw)
	for i := 1; i <= 9; i++ {
		value := ""
		if i-1 < len(args) {
			value = args[i-1]
		}
		content = strings.ReplaceAll(content, fmt.Sprintf("$%d", i), value)
	}
	for i, name := range s.Arguments {
		value := ""
		if i < len(args) {
			value = args[i]
		}
		content = strings.ReplaceAll(content, "$"+name, value)
	}
	content = strings.ReplaceAll(content, "${CLAUDE_SKILL_DIR}", filepath.ToSlash(s.BaseDir))
	if strings.TrimSpace(s.PluginRoot) != "" {
		content = strings.ReplaceAll(content, "${CLAUDE_PLUGIN_ROOT}", filepath.ToSlash(s.PluginRoot))
	}
	return content
}

// ShellExecutionDisabled reads Claude's user policy for dynamic skill context.
// A project cannot re-enable shell preprocessing once the user's global policy
// disabled it, which keeps cloned repositories from weakening the setting.
func ShellExecutionDisabled(configDir string) bool {
	home := ""
	if strings.TrimSpace(configDir) != "" {
		home = filepath.Dir(filepath.Clean(configDir))
	}
	paths := []string{}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if custom := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); custom != "" {
		paths = append(paths, filepath.Join(custom, "settings.json"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw struct {
			Disable bool `json:"disableSkillShellExecution"`
		}
		if json.Unmarshal(data, &raw) == nil && raw.Disable {
			return true
		}
	}
	return false
}

// ExpandShellCommands implements Claude Code's dynamic skill context syntax.
// Inline !`command` placeholders are recognized only at line start or after
// whitespace. Fenced ```! blocks execute as one multi-line shell script. The
// original skill is scanned once; command output is inserted as plain text and
// never rescanned for nested placeholders.
func ExpandShellCommands(ctx context.Context, s Skill, content, projectRoot string, allow bool) (string, error) {
	if !strings.Contains(content, "!`") && !strings.Contains(content, "```!") {
		return content, nil
	}
	const disabled = "[shell command execution disabled by policy]"
	var out strings.Builder
	for i := 0; i < len(content); {
		// Multi-line fenced command block.
		if strings.HasPrefix(content[i:], "```!") && (i == 0 || content[i-1] == '\n') {
			lineEnd := strings.IndexByte(content[i:], '\n')
			if lineEnd >= 0 {
				bodyStart := i + lineEnd + 1
				endRel := strings.Index(content[bodyStart:], "\n```")
				if endRel >= 0 {
					command := content[bodyStart : bodyStart+endRel]
					if !allow && s.Source != "builtin" {
						out.WriteString(disabled)
					} else {
						value, err := runSkillShell(ctx, s, projectRoot, command)
						if err != nil {
							return "", err
						}
						out.WriteString(value)
					}
					i = bodyStart + endRel + len("\n```")
					continue
				}
			}
		}
		// Inline dynamic context. Claude only recognizes ! after whitespace or
		// at the start of a line, never KEY=!`cmd`.
		if strings.HasPrefix(content[i:], "!`") && (i == 0 || content[i-1] == '\n' || content[i-1] == ' ' || content[i-1] == '\t') {
			end := strings.IndexByte(content[i+2:], '`')
			if end >= 0 {
				command := content[i+2 : i+2+end]
				if !allow && s.Source != "builtin" {
					out.WriteString(disabled)
				} else {
					value, err := runSkillShell(ctx, s, projectRoot, command)
					if err != nil {
						return "", err
					}
					out.WriteString(value)
				}
				i += 2 + end + 1
				continue
			}
		}
		out.WriteByte(content[i])
		i++
	}
	return out.String(), nil
}

func runSkillShell(ctx context.Context, s Skill, projectRoot, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}
	var cmd *exec.Cmd
	if strings.EqualFold(strings.TrimSpace(s.Shell), "powershell") {
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
		} else {
			cmd = exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", command)
		}
	} else if runtime.GOOS == "windows" {
		// Claude's default is bash. On Windows prefer Git Bash when present; if
		// it isn't installed, fail clearly instead of silently changing syntax.
		bash, err := exec.LookPath("bash")
		if err != nil {
			return "", fmt.Errorf("skill %s dynamic context requires bash (or shell: powershell): %w", s.Name, err)
		}
		cmd = exec.CommandContext(ctx, bash, "-lc", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "CLAUDE_SKILL_DIR="+s.BaseDir, "LILITH_SKILL_DIR="+s.BaseDir)
	if strings.TrimSpace(s.PluginRoot) != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_PLUGIN_ROOT="+s.PluginRoot)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("skill %s dynamic command failed: %w: %s", s.Name, err, strings.TrimSpace(stderr.String()))
	}
	const max = 1024 * 1024
	data := stdout.Bytes()
	if len(data) > max {
		data = append(append([]byte(nil), data[:max]...), []byte("\n[dynamic context truncated at 1 MiB]")...)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// ApplicableToPaths mirrors Claude's paths-scoped automatic skill visibility.
// Explicit user invocation is intentionally not restricted by paths.
func ApplicableToPaths(s Skill, paths []string) bool {
	if len(s.Paths) == 0 {
		return true
	}
	if len(paths) == 0 {
		return false
	}
	for _, actual := range paths {
		actual = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(actual), "./"))
		for _, pattern := range s.Paths {
			if skillGlobMatch(pattern, actual) {
				return true
			}
		}
	}
	return false
}

func skillGlobMatch(pattern, value string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	value = filepath.ToSlash(value)
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
			continue
		}
		if c == '?' {
			b.WriteString("[^/]")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, rune(c)) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('$')
	ok, _ := regexp.MatchString(b.String(), value)
	return ok
}
