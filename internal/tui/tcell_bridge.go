package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/gdamore/tcell/v2"
)

// terminalFrame is the complete visual state consumed by the Windows terminal
// backend. Frames are deliberately complete rather than incremental: Tcell can
// therefore blank every cell that disappeared from the previous view.
type terminalFrame struct {
	view         string
	captureMouse bool
	forceSync    bool
}

// headlessRoot keeps Bubble Tea as Lilith's state machine and command runtime,
// while delegating all terminal input and drawing to Tcell.
type headlessRoot struct {
	root   RootModel
	frames chan terminalFrame
}

func (m headlessRoot) Init() tea.Cmd {
	m.publish(true)
	return m.root.Init()
}

func (m headlessRoot) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.root.Update(msg)
	switch nextRoot := next.(type) {
	case RootModel:
		m.root = nextRoot
	case *RootModel:
		if nextRoot != nil {
			m.root = *nextRoot
		}
	}
	m.publish(frameNeedsSync(msg))
	return m, cmd
}

// View stays empty because Bubble Tea runs without a renderer in the Windows
// dual-runtime. The real RootModel view is published through terminalFrame.
func (m headlessRoot) View() string { return "" }

func (m headlessRoot) publish(forceSync bool) {
	if m.frames == nil {
		return
	}
	frame := terminalFrame{
		view:         m.root.View(),
		captureMouse: m.root.wantsMouseCapture(),
		forceSync:    forceSync,
	}
	publishLatestFrame(m.frames, frame)
}

// publishLatestFrame coalesces bursts from streaming responses. Rendering an
// obsolete intermediate frame would only waste work and make Windows Terminal
// visibly lag behind the model.
func publishLatestFrame(frames chan terminalFrame, frame terminalFrame) {
	select {
	case frames <- frame:
		return
	default:
	}

	// Preserve a pending physical redraw even when a newer streaming frame
	// replaces the older frame in the latest-only queue.
	select {
	case pending := <-frames:
		frame.forceSync = frame.forceSync || pending.forceSync
	default:
	}

	select {
	case frames <- frame:
	default:
	}
}

func frameNeedsSync(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return true
	case tea.KeyMsg:
		return msg.Paste || msg.Type == tea.KeyEnter
	default:
		return false
	}
}

// tcellInputState translates Tcell events into the Bubble Tea v1 messages used
// by Lilith's existing models. Bracketed paste is accumulated and emitted as a
// single KeyMsg so a large clipboard never becomes thousands of renders.
type tcellInputState struct {
	pasting          bool
	paste            strings.Builder
	pasteEndedWithCR bool
	lastButton       tcell.ButtonMask
}

func (s *tcellInputState) messages(event tcell.Event) []tea.Msg {
	switch event := event.(type) {
	case *tcell.EventPaste:
		if event.Start() {
			s.pasting = true
			s.paste.Reset()
			s.pasteEndedWithCR = false
			return nil
		}
		if !s.pasting {
			return nil
		}
		s.pasting = false
		text := s.paste.String()
		s.paste.Reset()
		s.pasteEndedWithCR = false
		if text == "" {
			return nil
		}
		return []tea.Msg{tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune(text),
			Paste: true,
		}}

	case *tcell.EventKey:
		if s.pasting {
			s.appendPasteKey(event)
			return nil
		}
		if msg, ok := tcellKeyMsg(event); ok {
			return []tea.Msg{msg}
		}

	case *tcell.EventResize:
		width, height := event.Size()
		return []tea.Msg{tea.WindowSizeMsg{Width: width, Height: height}}

	case *tcell.EventMouse:
		if msg, ok := s.mouseMsg(event); ok {
			return []tea.Msg{msg}
		}
	}
	return nil
}

func (s *tcellInputState) appendPasteKey(event *tcell.EventKey) {
	key := event.Key()
	if key == tcell.KeyRune {
		s.pasteEndedWithCR = false
		s.paste.WriteRune(event.Rune())
		return
	}
	switch key {
	case tcell.KeyEnter, tcell.KeyCtrlM:
		s.paste.WriteByte('\n')
		s.pasteEndedWithCR = true
	case tcell.KeyLF, tcell.KeyCtrlJ:
		if !s.pasteEndedWithCR {
			s.paste.WriteByte('\n')
		}
		s.pasteEndedWithCR = false
	case tcell.KeyTab, tcell.KeyCtrlI:
		s.pasteEndedWithCR = false
		s.paste.WriteByte('\t')
	default:
		s.pasteEndedWithCR = false
	}
}

func tcellKeyMsg(event *tcell.EventKey) (tea.KeyMsg, bool) {
	key := event.Key()
	modifiers := event.Modifiers()
	alt := modifiers&(tcell.ModAlt|tcell.ModMeta) != 0
	ctrl := modifiers&tcell.ModCtrl != 0
	shift := modifiers&tcell.ModShift != 0

	if key == tcell.KeyRune {
		r := event.Rune()
		if ctrl {
			if keyType, ok := controlRuneKey(r); ok {
				return tea.KeyMsg{Type: keyType, Alt: alt}, true
			}
		}
		if r == ' ' {
			// Bubble Tea's own parser reports a space as KeySpace while still
			// preserving the rune. Bubbles textarea inserts msg.Runes, so a
			// KeySpace without Runes is displayed as a no-op.
			return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}, true
		}
		if r == 0 {
			return tea.KeyMsg{}, false
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: alt}, true
	}

	// Tcell's control-key range is intentionally aligned with ASCII C0 values.
	if key >= tcell.KeyCtrlSpace && key <= tcell.KeyCtrlUnderscore {
		return tea.KeyMsg{Type: tea.KeyType(key - tcell.KeyCtrlSpace), Alt: alt}, true
	}

	switch key {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	case tcell.KeyEnter:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}, true
	case tcell.KeyTab:
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case tcell.KeyBacktab:
		return tea.KeyMsg{Type: tea.KeyShiftTab, Alt: alt}, true
	case tcell.KeyEsc:
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}, true
	case tcell.KeyDelete:
		return tea.KeyMsg{Type: tea.KeyDelete, Alt: alt}, true
	case tcell.KeyInsert:
		return tea.KeyMsg{Type: tea.KeyInsert, Alt: alt}, true
	case tcell.KeyPgUp:
		if ctrl {
			return tea.KeyMsg{Type: tea.KeyCtrlPgUp, Alt: alt}, true
		}
		return tea.KeyMsg{Type: tea.KeyPgUp, Alt: alt}, true
	case tcell.KeyPgDn:
		if ctrl {
			return tea.KeyMsg{Type: tea.KeyCtrlPgDown, Alt: alt}, true
		}
		return tea.KeyMsg{Type: tea.KeyPgDown, Alt: alt}, true
	case tcell.KeyUp:
		return tea.KeyMsg{Type: directionalKey(tea.KeyUp, tea.KeyCtrlUp, tea.KeyShiftUp, tea.KeyCtrlShiftUp, ctrl, shift), Alt: alt}, true
	case tcell.KeyDown:
		return tea.KeyMsg{Type: directionalKey(tea.KeyDown, tea.KeyCtrlDown, tea.KeyShiftDown, tea.KeyCtrlShiftDown, ctrl, shift), Alt: alt}, true
	case tcell.KeyLeft:
		return tea.KeyMsg{Type: directionalKey(tea.KeyLeft, tea.KeyCtrlLeft, tea.KeyShiftLeft, tea.KeyCtrlShiftLeft, ctrl, shift), Alt: alt}, true
	case tcell.KeyRight:
		return tea.KeyMsg{Type: directionalKey(tea.KeyRight, tea.KeyCtrlRight, tea.KeyShiftRight, tea.KeyCtrlShiftRight, ctrl, shift), Alt: alt}, true
	case tcell.KeyHome:
		return tea.KeyMsg{Type: directionalKey(tea.KeyHome, tea.KeyCtrlHome, tea.KeyShiftHome, tea.KeyCtrlShiftHome, ctrl, shift), Alt: alt}, true
	case tcell.KeyEnd:
		return tea.KeyMsg{Type: directionalKey(tea.KeyEnd, tea.KeyCtrlEnd, tea.KeyShiftEnd, tea.KeyCtrlShiftEnd, ctrl, shift), Alt: alt}, true
	}

	if key >= tcell.KeyF1 && key <= tcell.KeyF20 {
		return tea.KeyMsg{
			Type: tea.KeyType(int(tea.KeyF1) + int(key-tcell.KeyF1)),
			Alt:  alt,
		}, true
	}

	return tea.KeyMsg{}, false
}

func controlRuneKey(r rune) (tea.KeyType, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return tea.KeyType(r-'a') + tea.KeyCtrlA, true
	case r >= 'A' && r <= 'Z':
		return tea.KeyType(r-'A') + tea.KeyCtrlA, true
	case r == ' ' || r == '@':
		return tea.KeyCtrlAt, true
	case r == '[':
		return tea.KeyCtrlOpenBracket, true
	case r == '\\':
		return tea.KeyCtrlBackslash, true
	case r == ']':
		return tea.KeyCtrlCloseBracket, true
	case r == '^':
		return tea.KeyCtrlCaret, true
	case r == '_':
		return tea.KeyCtrlUnderscore, true
	default:
		return 0, false
	}
}

func directionalKey(normal, control, shifted, controlShift tea.KeyType, ctrl, shift bool) tea.KeyType {
	switch {
	case ctrl && shift:
		return controlShift
	case ctrl:
		return control
	case shift:
		return shifted
	default:
		return normal
	}
}

func (s *tcellInputState) mouseMsg(event *tcell.EventMouse) (tea.MouseMsg, bool) {
	x, y := event.Position()
	buttons := event.Buttons()
	modifiers := event.Modifiers()
	base := tea.MouseEvent{
		X:     x,
		Y:     y,
		Shift: modifiers&tcell.ModShift != 0,
		Alt:   modifiers&(tcell.ModAlt|tcell.ModMeta) != 0,
		Ctrl:  modifiers&tcell.ModCtrl != 0,
	}

	if button, ok := wheelButton(buttons); ok {
		base.Action = tea.MouseActionPress
		base.Button = button
		return tea.MouseMsg(base), true
	}

	current := physicalButtonMask(buttons)
	switch {
	case current == tcell.ButtonNone && s.lastButton != tcell.ButtonNone:
		base.Action = tea.MouseActionRelease
		base.Button = bubbleMouseButton(s.lastButton)
		s.lastButton = tcell.ButtonNone
		return tea.MouseMsg(base), true

	case current != tcell.ButtonNone && s.lastButton == tcell.ButtonNone:
		s.lastButton = current
		base.Action = tea.MouseActionPress
		base.Button = bubbleMouseButton(current)
		return tea.MouseMsg(base), true

	case current != tcell.ButtonNone:
		s.lastButton = current
		base.Action = tea.MouseActionMotion
		base.Button = bubbleMouseButton(current)
		return tea.MouseMsg(base), true

	default:
		base.Action = tea.MouseActionMotion
		base.Button = tea.MouseButtonNone
		return tea.MouseMsg(base), true
	}
}

func physicalButtonMask(buttons tcell.ButtonMask) tcell.ButtonMask {
	for _, button := range []tcell.ButtonMask{
		tcell.Button1,
		tcell.Button2,
		tcell.Button3,
		tcell.Button4,
		tcell.Button5,
		tcell.Button6,
		tcell.Button7,
		tcell.Button8,
	} {
		if buttons&button != 0 {
			return button
		}
	}
	return tcell.ButtonNone
}

func wheelButton(buttons tcell.ButtonMask) (tea.MouseButton, bool) {
	switch {
	case buttons&tcell.WheelUp != 0:
		return tea.MouseButtonWheelUp, true
	case buttons&tcell.WheelDown != 0:
		return tea.MouseButtonWheelDown, true
	case buttons&tcell.WheelLeft != 0:
		return tea.MouseButtonWheelLeft, true
	case buttons&tcell.WheelRight != 0:
		return tea.MouseButtonWheelRight, true
	default:
		return tea.MouseButtonNone, false
	}
}

func bubbleMouseButton(button tcell.ButtonMask) tea.MouseButton {
	switch button {
	case tcell.Button1:
		return tea.MouseButtonLeft
	case tcell.Button2:
		return tea.MouseButtonRight
	case tcell.Button3:
		return tea.MouseButtonMiddle
	case tcell.Button4:
		return tea.MouseButtonForward
	case tcell.Button5:
		return tea.MouseButtonBackward
	case tcell.Button6:
		return tea.MouseButton10
	case tcell.Button7, tcell.Button8:
		return tea.MouseButton11
	default:
		return tea.MouseButtonNone
	}
}

// renderTCell converts the ANSI/Lipgloss view into terminal cells. The logical
// screen is cleared before every frame, so rows that disappear are explicitly
// replaced with blanks instead of being left to an incremental string renderer.
// It returns the last occupied row count, used to force a physical Sync when a
// frame contracts vertically.
func renderTCell(screen tcell.Screen, view string, previousOccupiedHeight int, forceSync bool) int {
	width, height := screen.Size()
	if width <= 0 || height <= 0 {
		return 0
	}

	buffer := cellbuf.NewBuffer(width, height)
	cellbuf.SetContent(buffer, view)

	screen.Clear()
	occupiedHeight := 0
	for y := 0; y < height; y++ {
		rowOccupied := false
		for x := 0; x < width; x++ {
			cell := buffer.Cell(x, y)
			if cell == nil || cell.Width <= 0 {
				continue
			}

			r := cell.Rune
			combining := cell.Comb
			if r == 0 {
				r = ' '
			}
			if cell.Style.Attrs.Contains(cellbuf.ConcealAttr) {
				r = ' '
				combining = nil
			}
			if r != ' ' || len(combining) > 0 || !cell.Style.Empty() || !cell.Link.Empty() {
				rowOccupied = true
			}

			screen.SetContent(x, y, r, combining, tcellStyle(cell.Style, cell.Link))
			if cell.Width > 1 {
				x += cell.Width - 1
			}
		}
		if rowOccupied {
			occupiedHeight = y + 1
		}
	}

	screen.HideCursor()
	if forceSync || occupiedHeight < previousOccupiedHeight {
		screen.Sync()
	} else {
		screen.Show()
	}
	return occupiedHeight
}

func tcellStyle(style cellbuf.Style, link cellbuf.Link) tcell.Style {
	result := tcell.StyleDefault
	if style.Fg != nil {
		result = result.Foreground(tcellColor(style.Fg))
	}
	if style.Bg != nil {
		result = result.Background(tcellColor(style.Bg))
	}
	result = result.
		Bold(style.Attrs.Contains(cellbuf.BoldAttr)).
		Dim(style.Attrs.Contains(cellbuf.FaintAttr)).
		Italic(style.Attrs.Contains(cellbuf.ItalicAttr)).
		Blink(style.Attrs.Contains(cellbuf.SlowBlinkAttr) || style.Attrs.Contains(cellbuf.RapidBlinkAttr)).
		Reverse(style.Attrs.Contains(cellbuf.ReverseAttr)).
		StrikeThrough(style.Attrs.Contains(cellbuf.StrikethroughAttr))

	switch style.UlStyle {
	case cellbuf.SingleUnderline:
		result = result.Underline(tcell.UnderlineStyleSolid)
	case cellbuf.DoubleUnderline:
		result = result.Underline(tcell.UnderlineStyleDouble)
	case cellbuf.CurlyUnderline:
		result = result.Underline(tcell.UnderlineStyleCurly)
	case cellbuf.DottedUnderline:
		result = result.Underline(tcell.UnderlineStyleDotted)
	case cellbuf.DashedUnderline:
		result = result.Underline(tcell.UnderlineStyleDashed)
	}
	if style.Ul != nil {
		result = result.Underline(tcellColor(style.Ul))
	}
	if link.URL != "" {
		result = result.Url(link.URL)
		if id := hyperlinkID(link.Params); id != "" {
			result = result.UrlId(id)
		}
	}
	return result
}

type rgbaColor interface {
	RGBA() (r, g, b, a uint32)
}

func tcellColor(color rgbaColor) tcell.Color {
	if color == nil {
		return tcell.ColorDefault
	}
	r, g, b, alpha := color.RGBA()
	if alpha == 0 {
		return tcell.ColorDefault
	}
	return tcell.NewRGBColor(int32(r>>8), int32(g>>8), int32(b>>8))
}

func hyperlinkID(params string) string {
	for _, part := range strings.Split(params, ":") {
		if strings.HasPrefix(part, "id=") {
			return strings.TrimPrefix(part, "id=")
		}
	}
	return ""
}
