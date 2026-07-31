package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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
	frames   *tviewFrameQueue
	stopped  chan struct{}
	stopOnce sync.Once
}

const tviewFrameInterval = time.Second / 30

type tviewFrame struct {
	view         string
	captureMouse bool
}

// tviewFrameQueue keeps at most one frame waiting for the physical terminal.
// Rendering through tview.QueueUpdateDraw is synchronous: if the terminal is
// slow, waiting for it in modelLoop would also pause SSE reads, timers and key
// handling. Replacing an obsolete pending frame with the newest one keeps the
// state loop independent from terminal throughput.
type tviewFrameQueue struct {
	mu         sync.Mutex
	pending    tviewFrame
	hasPending bool
	ready      chan struct{}
}

func newTViewFrameQueue() *tviewFrameQueue {
	return &tviewFrameQueue{ready: make(chan struct{}, 1)}
}

func (q *tviewFrameQueue) publish(frame tviewFrame) {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.pending = frame
	q.hasPending = true
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *tviewFrameQueue) pop(stopped <-chan struct{}) (tviewFrame, bool) {
	if q == nil {
		return tviewFrame{}, false
	}
	for {
		q.mu.Lock()
		if q.hasPending {
			frame := q.pending
			q.pending = tviewFrame{}
			q.hasPending = false
			q.mu.Unlock()
			return frame, true
		}
		q.mu.Unlock()

		select {
		case <-q.ready:
		case <-stopped:
			return tviewFrame{}, false
		}
	}
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
		frames:   newTViewFrameQueue(),
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

	go runtime.renderLoop()
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

func (q *tviewMessageQueue) tryPop() (uikit.Msg, bool) {
	if q == nil {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, false
	}
	msg := q.items[0]
	q.items[0] = nil
	q.items = q.items[1:]
	if len(q.items) == 0 {
		q.items = nil
	}
	return msg, true
}

func (q *tviewMessageQueue) pop(stopped <-chan struct{}) (uikit.Msg, bool) {
	if q == nil {
		return nil, false
	}
	for {
		if msg, ok := q.tryPop(); ok {
			return msg, true
		}
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
	lastFrame := time.Now()
	dirty := false
	frameTimer := time.NewTimer(time.Hour)
	defer frameTimer.Stop()
	if !frameTimer.Stop() {
		<-frameTimer.C
	}
	var frameDeadline <-chan time.Time

	publishFrame := func() {
		if !dirty {
			return
		}
		r.frames.publish(tviewFrame{
			view:         r.root.View(),
			captureMouse: r.root.wantsMouseCapture(),
		})
		dirty = false
		lastFrame = time.Now()
		frameDeadline = nil
	}

	requestFrame := func() {
		dirty = true
		if frameDeadline != nil {
			return
		}
		wait := tviewFrameInterval - time.Since(lastFrame)
		if wait <= 0 {
			publishFrame()
			return
		}
		frameTimer.Reset(wait)
		frameDeadline = frameTimer.C
	}

	for {
		// Never starve a due frame behind a burst of provider chunks. The frame
		// itself is latest-only, so publishing it cannot block the state loop.
		if frameDeadline != nil {
			select {
			case <-frameDeadline:
				publishFrame()
				continue
			default:
			}
		}

		if msg, ok := r.messages.tryPop(); ok {
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

			// Start the next stream read/tool/timer before preparing a frame. A slow
			// terminal must never become backpressure for the provider connection.
			r.dispatch(cmd)
			// Runtime messages for the persistent chat keep flowing while /config,
			// /models or another screen is visible. They do not change that screen,
			// so repainting it for every hidden token would only waste terminal time.
			if r.root.chatVisible() || !chatRuntimeMsg(msg) {
				requestFrame()
			}
			continue
		}

		select {
		case <-r.messages.ready:
		case <-frameDeadline:
			publishFrame()
		case <-r.stopped:
			return
		}
	}
}

func (r *tviewRuntime) renderLoop() {
	for {
		frame, ok := r.frames.pop(r.stopped)
		if !ok {
			return
		}
		// QueueUpdateDraw intentionally lives only in this goroutine because it
		// waits for tview's event loop and the physical terminal to complete the
		// draw. While that happens modelLoop continues consuming SSE and timers.
		r.app.QueueUpdateDraw(func() {
			r.app.EnableMouse(frame.captureMouse)
			r.surface.setView(frame.view)
		})
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
