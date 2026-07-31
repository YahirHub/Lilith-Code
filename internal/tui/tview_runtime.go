package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/lilith/li/internal/tui/uikit"
	"github.com/rivo/tview"
)

// tviewRuntime owns Lilith's terminal event loop, input, paste handling, mouse
// capture, frame scheduling and rendering. The state machine is framework-neutral
// and uses only Lilith's internal uikit messages and commands.
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
	items []uikit.Msg
	ready chan struct{}
}

func newTViewMessageQueue() *tviewMessageQueue {
	return &tviewMessageQueue{ready: make(chan struct{}, 1)}
}

func (q *tviewMessageQueue) push(msg uikit.Msg) {
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

func (q *tviewMessageQueue) pop(stopped <-chan struct{}) (uikit.Msg, bool) {
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

func (r *tviewRuntime) send(msg uikit.Msg) {
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
		if _, quitting := msg.(uikit.QuitMsg); quitting {
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

func (r *tviewRuntime) dispatch(cmd uikit.Cmd) {
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
		if batch, ok := msg.(uikit.BatchMsg); ok {
			for _, child := range batch {
				r.dispatch(child)
			}
			return
		}
		r.send(msg)
	}()
}

// tviewModelSurface is Lilith's native tview primitive. TextView translates the
// ANSI styling produced by the internal style package into tcell styles, so no
// legacy renderer or external cell buffer remains in the terminal path.
type tviewModelSurface struct {
	*tview.TextView

	mu       sync.Mutex
	emit     func(uikit.Msg)
	lastSize struct{ width, height int }
}

func newTViewModelSurface(emit func(uikit.Msg)) *tviewModelSurface {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetWrap(false)
	view.SetScrollable(false)
	return &tviewModelSurface{TextView: view, emit: emit}
}

func translateViewForTView(view string) string {
	// User messages and source code may legitimately contain strings such as
	// [red]. Escape those literals before TranslateANSI inserts its own style
	// tags, otherwise TextView would interpret user content as formatting.
	return tview.TranslateANSI(tview.Escape(view))
}

func (s *tviewModelSurface) setView(view string) {
	s.mu.Lock()
	s.TextView.SetText(translateViewForTView(view))
	s.mu.Unlock()
}

func (s *tviewModelSurface) Draw(screen tcell.Screen) {
	_, _, width, height := s.GetRect()
	if width <= 0 || height <= 0 {
		return
	}

	s.mu.Lock()
	resized := s.lastSize.width != width || s.lastSize.height != height
	if resized {
		s.lastSize.width = width
		s.lastSize.height = height
	}
	s.TextView.Draw(screen)
	s.mu.Unlock()

	if resized && s.emit != nil {
		go s.emit(uikit.WindowSizeMsg{Width: width, Height: height})
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
		s.emit(uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune(text), Paste: true})
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

func tviewMouseMsg(action tview.MouseAction, event *tcell.EventMouse) (uikit.MouseMsg, bool) {
	x, y := event.Position()
	modifiers := event.Modifiers()
	msg := uikit.MouseEvent{
		X:     x,
		Y:     y,
		Shift: modifiers&tcell.ModShift != 0,
		Alt:   modifiers&(tcell.ModAlt|tcell.ModMeta) != 0,
		Ctrl:  modifiers&tcell.ModCtrl != 0,
	}

	switch action {
	case tview.MouseMove:
		msg.Action = uikit.MouseActionMotion
		msg.Button = uikit.MouseButtonNone
	case tview.MouseLeftDown:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonLeft
	case tview.MouseLeftUp:
		msg.Action = uikit.MouseActionRelease
		msg.Button = uikit.MouseButtonLeft
	case tview.MouseMiddleDown:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonMiddle
	case tview.MouseMiddleUp:
		msg.Action = uikit.MouseActionRelease
		msg.Button = uikit.MouseButtonMiddle
	case tview.MouseRightDown:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonRight
	case tview.MouseRightUp:
		msg.Action = uikit.MouseActionRelease
		msg.Button = uikit.MouseButtonRight
	case tview.MouseScrollUp:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonWheelUp
	case tview.MouseScrollDown:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonWheelDown
	case tview.MouseScrollLeft:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonWheelLeft
	case tview.MouseScrollRight:
		msg.Action = uikit.MouseActionPress
		msg.Button = uikit.MouseButtonWheelRight
	default:
		// Click and double-click actions are synthesized after the down/up
		// events. Ignoring them prevents duplicate activations in legacy models.
		return uikit.MouseMsg{}, false
	}
	return uikit.MouseMsg(msg), true
}
