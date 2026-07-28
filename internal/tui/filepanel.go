package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FilePanel es la ventana plegable que muestra en vivo lo que una herramienta
// de archivo está escribiendo: creación (todo en verde) o edición (diff con
// líneas añadidas en verde y eliminadas en rojo, al estilo GitHub).
type filePanelEdit struct {
	Old string
	New string
}

type FilePanel struct {
	Tool     string // write_file | str_replace
	CallID   string
	Index    int
	Path     string
	Content  string          // write_file: contenido en construcción
	Old, New string          // str_replace: bloque único/parcial durante streaming
	Edits    []filePanelEdit // str_replace: lote completo de cambios, al estilo pi.dev
	Done     bool
	Failed   bool
	Skipped  bool
	// Superseded marca un panel que el backend abandonó a mitad del stream
	// (Codex reintentó la tool call en otro output_index sin cerrar la
	// anterior). Se dibuja colapsado y con la nota "reintentado" en vez
	// de dejar el shimmer "escribiendo…" para siempre.
	Superseded bool
	Result     string
	// Expanded muestra el archivo completo. Por defecto el panel está en
	// vista previa: crece con el contenido hasta el límite y nunca se pliega solo.
	Expanded bool
}

// previewLines conserva el límite máximo histórico de la vista previa.
const previewLines = 12

// IsFileTool indica si una herramienta se representa como ventana de archivo.
func IsFileTool(name string) bool {
	return name == "write_file" || name == "str_replace"
}

// Update refresca el panel con los argumentos (posiblemente incompletos) que
// el modelo lleva emitidos hasta ahora.
func (p *FilePanel) Update(rawArgs string) {
	if v, ok := partialJSONString(rawArgs, "path"); ok {
		p.Path = v
	}
	switch p.Tool {
	case "write_file":
		if v, ok := partialJSONString(rawArgs, "content"); ok {
			p.Content = v
		}
	case "str_replace":
		// Mientras los argumentos siguen llegando mantenemos el preview del
		// par simple. Cuando el JSON queda completo, preferimos edits[] para
		// representar correctamente los lotes multi-edición que usa pi.dev.
		if v, ok := partialJSONString(rawArgs, "old"); ok {
			p.Old = v
		}
		if v, ok := partialJSONString(rawArgs, "new"); ok {
			p.New = v
		}
		if edits := parsePanelEdits(rawArgs); len(edits) > 0 {
			p.Edits = edits
		}
	}
}

func parsePanelEdits(rawArgs string) []filePanelEdit {
	var args map[string]any
	if json.Unmarshal([]byte(rawArgs), &args) != nil {
		return nil
	}
	var raw []any
	if value, ok := args["edits"]; ok {
		switch v := value.(type) {
		case []any:
			raw = v
		case string:
			_ = json.Unmarshal([]byte(v), &raw)
		}
	}
	out := make([]filePanelEdit, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		old, _ := panelStringField(m, "old", "oldText")
		newText, newOK := panelStringField(m, "new", "newText")
		if old == "" || !newOK {
			continue
		}
		out = append(out, filePanelEdit{Old: old, New: newText})
	}
	return out
}

func panelStringField(values map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := values[name]; ok {
			s, ok := value.(string)
			return s, ok
		}
	}
	return "", false
}

func (p *FilePanel) replacements() []filePanelEdit {
	if len(p.Edits) > 0 {
		return p.Edits
	}
	if p.Old == "" && p.New == "" {
		return nil
	}
	return []filePanelEdit{{Old: p.Old, New: p.New}}
}

// Finish cierra el panel con el resultado real de la herramienta.
func (p *FilePanel) Finish(result string) {
	p.Done = true
	p.Result = strings.TrimSpace(result)
	p.Failed = strings.HasPrefix(p.Result, "error:")
	p.Skipped = strings.HasPrefix(p.Result, "FILE_EXISTS:")
}

func (p *FilePanel) title() string {
	path := p.Path
	if path == "" {
		path = "(file)"
	}
	// Mimic a bash invocation: "$ write_file path" / "$ str_replace path".
	verb := p.Tool
	if verb == "" {
		verb = "edit"
	}
	prefix := "$ " + verb + " " + path
	if p.Done {
		switch {
		case p.Failed:
			return prefix + "   [failed]"
		case p.Skipped:
			return prefix + "   [skipped]"
		case p.Superseded:
			return prefix + "   [retried]"
		default:
			return prefix + "   [done]"
		}
	}
	return prefix
}

// MarkSuperseded cierra el panel como abandonado por el backend.
func (p *FilePanel) MarkSuperseded() {
	p.Done = true
	p.Superseded = true
	p.Expanded = false
	if p.Result == "" {
		p.Result = "retried by the model"
	}
}

// stats devuelve el número de líneas añadidas y eliminadas.
func (p *FilePanel) stats() (int, int) {
	if p.Tool == "write_file" {
		return len(splitLines(p.Content)), 0
	}
	add, del := 0, 0
	for _, edit := range p.replacements() {
		for _, l := range diffLines(splitLines(edit.Old), splitLines(edit.New)) {
			switch l.op {
			case '+':
				add++
			case '-':
				del++
			}
		}
	}
	return add, del
}

// View renderiza la ventana. `selected` marca el panel enfocado por teclado.
func (p *FilePanel) View(s Styles, width int, selected bool) string {
	if width < 24 {
		width = 24
	}
	inner := width - 4
	t := s.Theme

	arrow := "▸"
	if p.Expanded {
		arrow = "▾"
	}
	add, del := p.stats()
	counts := lipgloss.NewStyle().Foreground(t.Success).Render(fmt.Sprintf("+%d", add))
	if del > 0 {
		counts += " " + lipgloss.NewStyle().Foreground(t.Danger).Render(fmt.Sprintf("-%d", del))
	}
	head := lipgloss.NewStyle().Foreground(t.Primary).Bold(true).Render(arrow+" "+p.title()) + "  " + counts
	if !p.Done {
		// Terminal-style "running…" cue instead of an emoji spinner.
		head += "  " + s.Muted.Render("• running")
	}
	if selected {
		hint := "(ctrl+o expand)"
		if p.Expanded {
			hint = "(ctrl+o preview)"
		}
		head += "  " + s.Muted.Render(hint)
	}

	body := head
	if lines := p.renderBody(s, inner); lines != "" {
		body += "\n" + lines
	}
	if p.Done && p.Result != "" {
		style := s.Muted
		if p.Failed {
			style = s.Danger
		} else if p.Skipped {
			style = s.Warning
		}
		body += "\n" + style.Render(firstLine(p.Result))
	}

	border := t.Border
	if selected {
		border = t.Primary
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width - 2).
		Render(body)
}

func (p *FilePanel) renderBody(s Styles, inner int) string {
	t := s.Theme
	green := lipgloss.NewStyle().Foreground(t.Success)
	red := lipgloss.NewStyle().Foreground(t.Danger)
	ctx := lipgloss.NewStyle().Foreground(t.Muted)

	var out []string
	if p.Tool == "write_file" {
		for _, l := range splitLines(p.Content) {
			out = append(out, green.Render("+ "+clip(l, inner-2)))
		}
	} else {
		for editIndex, edit := range p.replacements() {
			if editIndex > 0 {
				out = append(out, ctx.Render("  ···"))
			}
			for _, d := range diffLines(splitLines(edit.Old), splitLines(edit.New)) {
				text := clip(d.text, inner-2)
				switch d.op {
				case '+':
					out = append(out, green.Render("+ "+text))
				case '-':
					out = append(out, red.Render("- "+text))
				default:
					out = append(out, ctx.Render("  "+text))
				}
			}
		}
	}
	if p.Expanded {
		if len(out) == 0 {
			return ""
		}
		return strings.Join(out, "\n")
	}

	// Vista previa adaptativa: con poco contenido ocupa únicamente las filas
	// necesarias; al alcanzar el límite conserva una ventana de las líneas más
	// recientes y reserva una fila para indicar cuánto quedó oculto.
	view, hidden := cappedTailPreview(out, previewLines)
	if hidden > 0 {
		view = append([]string{ctx.Render(fmt.Sprintf("… %d more lines above (ctrl+o to expand)", hidden))}, view...)
	}
	return strings.Join(view, "\n")
}

func clip(s string, max int) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	if max < 8 {
		max = 8
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

type diffLine struct {
	op   byte // '+', '-', ' '
	text string
}

// diffLines calcula un diff por líneas con LCS clásico (los bloques de
// str_replace son pequeños, así que la matriz es barata).
func diffLines(a, b []string) []diffLine {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{'-', a[i]})
			i++
		default:
			out = append(out, diffLine{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{'+', b[j]})
	}
	return out
}

// partialJSONString extrae el valor de una clave string de un JSON que puede
// estar incompleto (el modelo aún lo está emitiendo por SSE).
func partialJSONString(raw, key string) (string, bool) {
	needle := `"` + key + `"`
	idx := strings.Index(raw, needle)
	if idx < 0 {
		return "", false
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == ':' || raw[i] == '\n' || raw[i] == '\t' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return "", false
	}
	i++
	var b strings.Builder
	for i < len(raw) {
		c := raw[i]
		if c == '"' {
			return b.String(), true
		}
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(raw) {
			break // escape truncado: devolvemos lo acumulado
		}
		switch raw[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'u':
			if i+5 < len(raw) {
				var r rune
				if _, err := fmt.Sscanf(raw[i+2:i+6], "%04x", &r); err == nil {
					b.WriteRune(r)
				}
				i += 4
			}
		default:
			b.WriteByte(raw[i+1])
		}
		i += 2
	}
	return b.String(), true
}
