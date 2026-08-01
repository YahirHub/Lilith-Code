package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/lilith/li/internal/rewind"
	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/lilith/li/internal/tui/uikit/textinput"
)

type forkDestinationStage int

const (
	forkDestinationBrowse forkDestinationStage = iota
	forkDestinationCreateFolder
	forkDestinationWorking
)

type forkBrowserEntryKind int

const (
	forkEntryUseCurrent forkBrowserEntryKind = iota
	forkEntryCreateFolder
	forkEntryParent
	forkEntryDrive
	forkEntryDirectory
)

type forkBrowserEntry struct {
	kind  forkBrowserEntryKind
	label string
	path  string
}

// ForkDestinationModel lets /fork choose an explicit empty directory without
// depending on a desktop file picker. Every action is available by keyboard,
// while Tcell mouse reporting adds clicks and wheel navigation when supported
// by the local terminal or SSH client.
type ForkDestinationModel struct {
	ctx        *AppContext
	chat       *ChatModel
	title      string
	sourceRoot string
	currentDir string
	entries    []forkBrowserEntry
	cursor     int
	stage      forkDestinationStage
	folderName textinput.Model
	err        string
}

func NewForkDestinationScreen(ctx *AppContext, chat *ChatModel, title string) *ForkDestinationModel {
	input := textinput.New()
	input.Prompt = "❯ "
	input.Placeholder = "Nombre de la carpeta"
	input.CharLimit = 255
	input.Focus()

	start := ""
	sourceRoot := ""
	if chat != nil {
		sourceRoot = rewind.ResolveWorkspaceRoot(chat.project)
		start = filepath.Dir(filepath.Clean(sourceRoot))
	}
	if strings.TrimSpace(start) == "" || start == "." {
		start, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	if strings.TrimSpace(sourceRoot) != "" {
		if abs, err := filepath.Abs(sourceRoot); err == nil {
			sourceRoot = abs
		}
		sourceRoot = filepath.Clean(sourceRoot)
	}

	m := &ForkDestinationModel{
		ctx:        ctx,
		chat:       chat,
		title:      strings.TrimSpace(title),
		sourceRoot: sourceRoot,
		currentDir: filepath.Clean(start),
		folderName: input,
	}
	m.reload()
	return m
}

func (m *ForkDestinationModel) Init() uikit.Cmd { return textinput.Blink }

func (m *ForkDestinationModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case uikit.WindowSizeMsg:
		m.ctx.Width, m.ctx.Height = v.Width, v.Height
		return m, nil
	case uikit.MouseMsg:
		return m.updateMouse(v)
	case uikit.KeyMsg:
		switch m.stage {
		case forkDestinationBrowse:
			return m.updateBrowseKey(v)
		case forkDestinationCreateFolder:
			return m.updateCreateFolderKey(v)
		case forkDestinationWorking:
			return m, nil
		}
	}

	if m.stage == forkDestinationCreateFolder {
		var cmd uikit.Cmd
		m.folderName, cmd = m.folderName.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ForkDestinationModel) updateMouse(msg uikit.MouseMsg) (uikit.Model, uikit.Cmd) {
	e := uikit.MouseEvent(msg)
	if m.stage == forkDestinationBrowse && e.IsWheel() {
		switch e.Button {
		case uikit.MouseButtonWheelUp:
			m.moveCursor(-1)
		case uikit.MouseButtonWheelDown:
			m.moveCursor(1)
		}
		return m, nil
	}
	e, ok := mouseLeftPress(msg)
	if !ok {
		return m, nil
	}

	switch m.stage {
	case forkDestinationBrowse:
		rect, offset := m.browserListGeometry()
		if !rect.contains(e.X, e.Y) {
			return m, nil
		}
		index := offset + e.Y - rect.y
		if index < 0 || index >= len(m.entries) {
			return m, nil
		}
		m.cursor = index
		return m.activateCurrentEntry()
	case forkDestinationCreateFolder:
		_, hits := m.createFolderLayout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return m, nil
		}
		switch hit.id {
		case "fork-folder-input":
			return m, m.folderName.Focus()
		case "fork-folder-create":
			return m.createFolder()
		case "fork-folder-cancel":
			m.stage = forkDestinationBrowse
			m.err = ""
			return m, nil
		}
	}
	return m, nil
}

func (m *ForkDestinationModel) updateBrowseKey(key uikit.KeyMsg) (uikit.Model, uikit.Cmd) {
	switch key.String() {
	case "esc", "q":
		return m, switchToChat()
	case "up", "ctrl+p", "k":
		m.moveCursor(-1)
	case "down", "ctrl+n", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-10)
	case "pgdown":
		m.moveCursor(10)
	case "home":
		m.cursor = 0
	case "end":
		if len(m.entries) > 0 {
			m.cursor = len(m.entries) - 1
		}
	case "backspace", "left", "alt+left", "h":
		return m.openParent()
	case "n":
		return m.beginCreateFolder()
	case "s":
		return m.selectCurrentDirectory()
	case "enter", "right", "l":
		return m.activateCurrentEntry()
	}
	return m, nil
}

func (m *ForkDestinationModel) updateCreateFolderKey(key uikit.KeyMsg) (uikit.Model, uikit.Cmd) {
	switch key.String() {
	case "esc":
		m.stage = forkDestinationBrowse
		m.err = ""
		return m, nil
	case "enter":
		return m.createFolder()
	}
	var cmd uikit.Cmd
	m.folderName, cmd = m.folderName.Update(key)
	return m, cmd
}

func (m *ForkDestinationModel) moveCursor(delta int) {
	if len(m.entries) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clampInt(m.cursor+delta, 0, len(m.entries)-1)
}

func (m *ForkDestinationModel) activateCurrentEntry() (uikit.Model, uikit.Cmd) {
	if len(m.entries) == 0 || m.cursor < 0 || m.cursor >= len(m.entries) {
		return m, nil
	}
	entry := m.entries[m.cursor]
	switch entry.kind {
	case forkEntryUseCurrent:
		return m.selectCurrentDirectory()
	case forkEntryCreateFolder:
		return m.beginCreateFolder()
	case forkEntryParent, forkEntryDrive, forkEntryDirectory:
		return m.openDirectory(entry.path)
	default:
		return m, nil
	}
}

func (m *ForkDestinationModel) beginCreateFolder() (uikit.Model, uikit.Cmd) {
	m.stage = forkDestinationCreateFolder
	m.err = ""
	m.folderName.SetValue("")
	return m, m.folderName.Focus()
}

func (m *ForkDestinationModel) createFolder() (uikit.Model, uikit.Cmd) {
	name := strings.TrimSpace(m.folderName.Value())
	if err := validateForkFolderName(name); err != nil {
		m.err = err.Error()
		return m, nil
	}
	path := filepath.Join(m.currentDir, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			m.err = "Ya existe una carpeta con ese nombre. Elige otro nombre o vuelve para abrirla."
		} else {
			m.err = "No se pudo crear la carpeta: " + err.Error()
		}
		return m, nil
	}
	m.stage = forkDestinationBrowse
	m.currentDir = filepath.Clean(path)
	m.cursor = 0
	m.err = ""
	m.reload()
	return m, nil
}

func (m *ForkDestinationModel) selectCurrentDirectory() (uikit.Model, uikit.Cmd) {
	if err := validateForkSelectedDirectory(m.currentDir, m.sourceRoot); err != nil {
		m.err = err.Error()
		return m, nil
	}
	if m.chat == nil {
		m.err = "La conversación activa ya no está disponible."
		return m, nil
	}
	m.stage = forkDestinationWorking
	m.err = ""
	return m, m.chat.startForkSessionAt(m.title, m.currentDir)
}

func (m *ForkDestinationModel) openParent() (uikit.Model, uikit.Cmd) {
	parent := filepath.Dir(m.currentDir)
	if filepath.Clean(parent) == filepath.Clean(m.currentDir) {
		return m, nil
	}
	return m.openDirectory(parent)
}

func (m *ForkDestinationModel) openDirectory(path string) (uikit.Model, uikit.Cmd) {
	if strings.TrimSpace(path) == "" {
		return m, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		m.err = "Ruta inválida: " + err.Error()
		return m, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		m.err = "No se pudo abrir la carpeta: " + err.Error()
		return m, nil
	}
	if !info.IsDir() {
		m.err = "La ruta seleccionada no es una carpeta."
		return m, nil
	}
	m.currentDir = filepath.Clean(abs)
	m.cursor = 0
	m.err = ""
	m.reload()
	return m, nil
}

func (m *ForkDestinationModel) reload() {
	entries := []forkBrowserEntry{
		{kind: forkEntryUseCurrent, label: "Usar esta carpeta vacía", path: m.currentDir},
		{kind: forkEntryCreateFolder, label: "Crear una carpeta nueva", path: m.currentDir},
	}
	parent := filepath.Dir(m.currentDir)
	if filepath.Clean(parent) != filepath.Clean(m.currentDir) {
		entries = append(entries, forkBrowserEntry{kind: forkEntryParent, label: "..  Volver atrás", path: parent})
	} else if runtime.GOOS == "windows" {
		entries = append(entries, availableWindowsDrives(m.currentDir)...)
	}

	dirs, err := os.ReadDir(m.currentDir)
	if err != nil {
		m.entries = entries
		m.cursor = clampInt(m.cursor, 0, len(m.entries)-1)
		m.err = "No se pudo listar la carpeta: " + err.Error()
		return
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name())
	})
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries = append(entries, forkBrowserEntry{
			kind:  forkEntryDirectory,
			label: dir.Name() + string(os.PathSeparator),
			path:  filepath.Join(m.currentDir, dir.Name()),
		})
	}
	m.entries = entries
	m.cursor = clampInt(m.cursor, 0, len(m.entries)-1)
}

func availableWindowsDrives(current string) []forkBrowserEntry {
	currentVolume := strings.ToUpper(filepath.VolumeName(current))
	out := make([]forkBrowserEntry, 0, 4)
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		label := "Unidad " + root
		if strings.EqualFold(strings.TrimSuffix(root, `\`), currentVolume) {
			label += " (actual)"
		}
		out = append(out, forkBrowserEntry{kind: forkEntryDrive, label: label, path: root})
	}
	return out
}

func validateForkFolderName(name string) error {
	if name == "" {
		return errors.New("Escribe un nombre para la carpeta.")
	}
	if name == "." || name == ".." {
		return errors.New("Ese nombre de carpeta no es válido.")
	}
	if strings.ContainsAny(name, `/\\<>:"|?*`) || strings.ContainsRune(name, 0) {
		return errors.New("El nombre contiene caracteres no permitidos.")
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return errors.New("El nombre no puede terminar en espacio o punto.")
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return errors.New("Ese nombre está reservado por Windows.")
	}
	return nil
}

func validateForkSelectedDirectory(destination, project string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("ruta de destino inválida: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("no se pudo abrir la carpeta: %w", err)
	}
	if !info.IsDir() {
		return errors.New("la ruta seleccionada no es una carpeta")
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("no se pudo comprobar la carpeta: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("la carpeta debe estar vacía; crea una subcarpeta nueva o elige otra")
	}
	if strings.TrimSpace(project) != "" && pathContains(project, abs) {
		return errors.New("el fork no puede crearse dentro del proyecto original")
	}
	return nil
}

func pathContains(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func (m *ForkDestinationModel) View() string {
	switch m.stage {
	case forkDestinationCreateFolder:
		view, _ := m.createFolderLayout()
		return view
	case forkDestinationWorking:
		return m.workingView()
	default:
		return renderViewportSelector(m.ctx.Styles, m.browserSpec())
	}
}

func (m *ForkDestinationModel) browserSpec() viewportSelectorSpec {
	items := make([]viewportSelectorItem, 0, len(m.entries))
	for _, entry := range m.entries {
		items = append(items, viewportSelectorItem{Primary: entry.label})
	}
	subtitle := m.currentDir
	if m.title != "" {
		subtitle += " · título: " + m.title
	}
	return viewportSelectorSpec{
		Title:         "Destino del fork",
		Subtitle:      subtitle,
		SearchContent: "El destino final debe estar vacío y fuera del workspace original.",
		Items:         items,
		Selected:      m.cursor,
		EmptyText:     "No hay carpetas disponibles.",
		Footer:        "↑↓ navegar · Enter/clic abrir o elegir · N crear carpeta · Backspace/← atrás · Esc cancelar",
		Error:         m.err,
		ScreenWidth:   m.ctx.Width,
		ScreenHeight:  m.ctx.Height,
	}
}

func (m *ForkDestinationModel) browserListGeometry() (settingsRect, int) {
	spec := m.browserSpec()
	width := viewportSelectorWidth(spec.ScreenWidth)
	headerParts := []string{m.ctx.Styles.Accent.Render(spec.Title)}
	if strings.TrimSpace(spec.Subtitle) != "" {
		headerParts = append(headerParts, m.ctx.Styles.Muted.Render(spec.Subtitle))
	}
	header := strings.Join(headerParts, "\n")
	search := viewportSelectorSearch(m.ctx.Styles, spec.SearchContent, width)
	footer := m.ctx.Styles.Muted.Render(settingsFitSingleLine(spec.Footer, width))
	errorText := ""
	if strings.TrimSpace(spec.Error) != "" {
		errorText = m.ctx.Styles.Danger.Render(settingsFitSingleLine(spec.Error, width))
	}
	fixedHeight := 1 + tuistyle.Height(header) + tuistyle.Height(search) + 1 + tuistyle.Height(footer)
	if errorText != "" {
		fixedHeight += tuistyle.Height(errorText)
	}
	listHeight := spec.ScreenHeight - fixedHeight
	if listHeight < 1 {
		listHeight = 1
	}
	rendered := renderViewportSelectorItems(m.ctx.Styles, spec.Items, spec.Selected, width)
	offset := viewportSelectorOffset(rendered, listHeight)
	x := (spec.ScreenWidth - width) / 2
	if x < 0 {
		x = 0
	}
	y := 1 + tuistyle.Height(header) + tuistyle.Height(search) + 1
	return settingsRect{x: x, y: y, w: width, h: listHeight}, offset
}

func (m *ForkDestinationModel) createFolderLayout() (string, []settingsHit) {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	if w > 12 {
		m.folderName.Width = w - 6
	}
	canvas := newSettingsCanvas(w)
	canvas.block(settingsHeader(s, "Crear carpeta para el fork", m.currentDir))
	canvas.blank()
	canvas.line(s.Title.Render("Nombre de la carpeta"))
	canvas.block(settingsInput(s, settingsInputSpec{
		ID:      "fork-folder-input",
		Content: m.folderName.View(),
		Width:   w,
		Focused: true,
	}))
	canvas.blank()
	canvas.block(settingsButtonGroup(s, w,
		settingsButtonSpec{ID: "fork-folder-create", Label: "Crear y abrir", Focused: true},
		settingsButtonSpec{ID: "fork-folder-cancel", Label: "Volver"},
	))
	if m.err != "" {
		canvas.blank()
		canvas.line(s.Danger.Render("Error: " + settingsWrapPlain(m.err, w)))
	}
	canvas.blank()
	canvas.block(settingsFooter(s, "Enter crear · Esc volver · también puedes usar el ratón"))
	return canvas.render(m.ctx.Width)
}

func (m *ForkDestinationModel) workingView() string {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	canvas := newSettingsCanvas(w)
	canvas.block(settingsHeader(s, "Creando fork", m.currentDir))
	canvas.blank()
	canvas.line(s.Muted.Render("Capturando la conversación y materializando una copia independiente del proyecto…"))
	view, _ := canvas.render(m.ctx.Width)
	return view
}

var _ uikit.Model = (*ForkDestinationModel)(nil)
