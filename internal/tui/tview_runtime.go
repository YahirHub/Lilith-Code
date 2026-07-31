package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tviewRuntime makes tview the owner of the terminal event loop and rendering
// lifecycle. The existing Bubble Tea models remain a compatibility state engine
// during the UI migration, but Bubble Tea no longer owns stdin, stdout, the
// alternate screen, paste handling, mouse capture, or frame scheduling.
type tviewRuntime struct {
	app     *tview.Application
	surface *tviewModelSurface
	root    RootModel

	messages *tviewMessageQueue
	stopped  chan struct{}
	stopOnce sync.Once
}

func newTViewRuntime(root RootModel, screen tcell.Screen) *tviewRuntime {
	app := tview.NewApplication()
	if screen != nil {
		// SetScreen initializes a supplied screen immediately. Configure paste and
		// title afterwards so tview also applies them to this already-live screen.
		app.SetScreen(screen)
	}
	app.EnablePaste(true).
		SetTitle("Lilith Code").
		SetInputCapture(forwardTViewCtrlC)

	runtime := &tviewRuntime{
		app:      app,
		root:     root,
		messages: newTViewMessageQueue(),
		stopped:  make(chan struct{}),
	}
	runtime.surface = newTViewModelSurface(runtime.send)
	app.SetRoot(runtime.surface, true)
	return runtime
}

func runRootTView(root RootModel, screen tcell.Screen) error {
	runtime := newTViewRuntime(root, screen)
	runtime.surface.setView(root.View())
	runtime.app.EnableMouse(root.wantsMouseCapture())

	go runtime.modelLoop()
	runtime.dispatch(root.Init())

	interrupts := make(chan os.Signal, 1)
	notifyTViewSignals(interrupts)
	defer stopTViewSignals(interrupts)
	go func() {
		select {
		case <-interrupts:
			runtime.stop()
		case <-runtime.stopped:
		}
	}()

	err := runtime.app.Run()
	runtime.stop()
	return err
}

func (r *tviewRuntime) stop() {
	r.stopOnce.Do(func() {
		close(r.stopped)
		r.app.Stop()
	})
}

// tviewMessageQueue is an ordered, unbounded handoff between tview's physical
// event loop and Lilith's model loop. Enqueue never blocks the terminal, while a
// mutex-protected FIFO preserves key and paste ordering under redraw pressure.
type tviewMessageQueue struct {
	mu    sync.Mutex
	items []tea.Msg
	ready chan struct{}
}

func newTViewMessageQueue() *tviewMessageQueue {
	return &tviewMessageQueue{ready: make(chan struct{}, 1)}
}

func (q *tviewMessageQueue) push(msg tea.Msg) {
	if q == nil || msg == nil {
		return
	}
	q.mu.Lock()
	q.items = append(q.items, msg)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *tviewMessageQueue) pop(stopped <-chan struct{}) (tea.Msg, bool) {
	if q == nil {
		return nil, false
	}
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			msg := q.items[0]
			q.items[0] = nil
			q.items = q.items[1:]
			if len(q.items) == 0 {
				q.items = nil
			}
			q.mu.Unlock()
			return msg, true
		}
		q.mu.Unlock()

		select {
		case <-q.ready:
		case <-stopped:
			return nil, false
		}
	}
}

func (r *tviewRuntime) send(msg tea.Msg) {
	if msg == nil {
		return
	}
	select {
	case <-r.stopped:
		return
	default:
		r.messages.push(msg)
	}
}

// forwardTViewCtrlC returns a cloned Ctrl+C event. tview treats the original
// event as its own immediate-stop shortcut; cloning it tells Application to
// forward the key to Lilith instead, preserving the existing cancel/exit logic.
func forwardTViewCtrlC(event *tcell.EventKey) *tcell.EventKey {
	if event != nil && event.Key() == tcell.KeyCtrlC {
		return tcell.NewEventKey(event.Key(), event.Rune(), event.Modifiers())
	}
	return event
}

func (r *tviewRuntime) modelLoop() {
	for {
		msg, ok := r.messages.pop(r.stopped)
		if !ok {
			return
		}
		if _, quitting := msg.(tea.QuitMsg); quitting {
			r.stop()
			return
		}

		next, cmd := r.root.Update(msg)
		switch nextRoot := next.(type) {
		case RootModel:
			r.root = nextRoot
		case *RootModel:
			if nextRoot != nil {
				r.root = *nextRoot
			}
		default:
			// RootModel is the router and should always remain the top-level
			// model. Ignore an unexpected replacement rather than losing the
			// persistent chat state.
		}

		view := r.root.View()
		captureMouse := r.root.wantsMouseCapture()
		r.app.QueueUpdateDraw(func() {
			r.app.EnableMouse(captureMouse)
			r.surface.setView(view)
		})
		r.dispatch(cmd)
	}
}

func (r *tviewRuntime) dispatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				r.send(errMsg{err: fmt.Errorf("comando TUI: %v", recovered)})
			}
		}()
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				r.dispatch(child)
			}
			return
		}
		r.send(msg)
	}()
}

// tviewModelSurface is a native tview primitive which paints Lilith's complete
// ANSI frame into tcell cells. It also exposes tview's paste callback so a whole
// clipboard block reaches the model as one atomic KeyMsg.
type tviewModelSurface struct {
	*tview.Box

	mu       sync.RWMutex
	view     string
	emit     func(tea.Msg)
	lastSize struct{ width, height int }
}

func newTViewModelSurface(emit func(tea.Msg)) *tviewModelSurface {
	return &tviewModelSurface{
		Box:  tview.NewBox(),
		emit: emit,
	}
}

func (s *tviewModelSurface) setView(view string) {
	s.mu.Lock()
	s.view = view
	s.mu.Unlock()
}

func (s *tviewModelSurface) Draw(screen tcell.Screen) {
	x, y, width, height := s.GetRect()
	if width <= 0 || height <= 0 {
		return
	}

	s.mu.Lock()
	view := s.view
	resized := s.lastSize.width != width || s.lastSize.height != height
	if resized {
		s.lastSize.width = width
		s.lastSize.height = height
	}
	s.mu.Unlock()

	drawANSIRegion(screen, x, y, width, height, view)
	if resized && s.emit != nil {
		// Draw runs inside tview's event loop. Queue the resize back into the
		// model loop rather than mutating RootModel while a frame is painted.
		go s.emit(tea.WindowSizeMsg{Width: width, Height: height})
	}
}

func (s *tviewModelSurface) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return func(event *tcell.EventKey, _ func(tview.Primitive)) {
		if s.emit == nil || event == nil {
			return
		}
		if msg, ok := tcellKeyMsg(event); ok {
			s.emit(msg)
		}
	}
}

func (s *tviewModelSurface) PasteHandler() func(string, func(tview.Primitive)) {
	return func(text string, _ func(tview.Primitive)) {
		if s.emit == nil || text == "" {
			return
		}
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		s.emit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	}
}

func (s *tviewModelSurface) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(action tview.MouseAction, event *tcell.EventMouse, _ func(tview.Primitive)) (bool, tview.Primitive) {
		if s.emit == nil || event == nil {
			return false, nil
		}
		if msg, ok := tviewMouseMsg(action, event); ok {
			s.emit(msg)
			if action == tview.MouseLeftDown || action == tview.MouseMiddleDown || action == tview.MouseRightDown {
				return true, s
			}
			return true, nil
		}
		return false, nil
	}
}

func tviewMouseMsg(action tview.MouseAction, event *tcell.EventMouse) (tea.MouseMsg, bool) {
	x, y := event.Position()
	modifiers := event.Modifiers()
	msg := tea.MouseEvent{
		X:     x,
		Y:     y,
		Shift: modifiers&tcell.ModShift != 0,
		Alt:   modifiers&(tcell.ModAlt|tcell.ModMeta) != 0,
		Ctrl:  modifiers&tcell.ModCtrl != 0,
	}

	switch action {
	case tview.MouseMove:
		msg.Action = tea.MouseActionMotion
		msg.Button = tea.MouseButtonNone
	case tview.MouseLeftDown:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonLeft
	case tview.MouseLeftUp:
		msg.Action = tea.MouseActionRelease
		msg.Button = tea.MouseButtonLeft
	case tview.MouseMiddleDown:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonMiddle
	case tview.MouseMiddleUp:
		msg.Action = tea.MouseActionRelease
		msg.Button = tea.MouseButtonMiddle
	case tview.MouseRightDown:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonRight
	case tview.MouseRightUp:
		msg.Action = tea.MouseActionRelease
		msg.Button = tea.MouseButtonRight
	case tview.MouseScrollUp:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonWheelUp
	case tview.MouseScrollDown:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonWheelDown
	case tview.MouseScrollLeft:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonWheelLeft
	case tview.MouseScrollRight:
		msg.Action = tea.MouseActionPress
		msg.Button = tea.MouseButtonWheelRight
	default:
		// Click and double-click actions are synthesized after the down/up
		// events. Ignoring them prevents duplicate activations in legacy models.
		return tea.MouseMsg{}, false
	}
	return tea.MouseMsg(msg), true
}

func drawANSIRegion(screen tcell.Screen, originX, originY, width, height int, view string) {
	buffer := cellbuf.NewBuffer(width, height)
	cellbuf.SetContent(buffer, view)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := buffer.Cell(x, y)
			if cell == nil || cell.Width <= 0 {
				screen.SetContent(originX+x, originY+y, ' ', nil, tcell.StyleDefault)
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
			screen.SetContent(originX+x, originY+y, r, combining, tcellStyle(cell.Style, cell.Link))
			if cell.Width > 1 {
				x += cell.Width - 1
			}
		}
	}
	screen.HideCursor()
}
