//go:build windows

package tui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gdamore/tcell/v2"
)

const windowsTCellTerm = "xterm-256color"

// newWindowsTCellScreen deliberately selects Tcell's terminfo/VT backend.
// tcell.NewScreen may fall back to the legacy cScreen when TERM is absent;
// that backend does not emit bracketed-paste boundaries on Windows. Opening
// CONIN$/CONOUT$ through NewDevTty and supplying a known xterm terminfo keeps
// paste atomic while Tcell remains the sole physical terminal renderer.
func newWindowsTCellScreen() (tcell.Screen, error) {
	tty, err := tcell.NewDevTty()
	if err != nil {
		return nil, fmt.Errorf("abrir consola VT para Tcell: %w", err)
	}

	terminfo, err := tcell.LookupTerminfo(windowsTCellTerm)
	if err != nil {
		_ = tty.Close()
		return nil, fmt.Errorf("cargar terminfo %s: %w", windowsTCellTerm, err)
	}

	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(tty, terminfo)
	if err != nil {
		_ = tty.Close()
		return nil, fmt.Errorf("crear pantalla Tcell VT: %w", err)
	}
	return screen, nil
}

// RunRoot uses a dual runtime on Windows: Bubble Tea continues running every
// Lilith model and command headlessly, while Tcell exclusively owns console
// input and cell rendering. Keeping a single terminal owner prevents the stale
// input rows produced by Bubble Tea's incremental Windows renderer.
func RunRoot(root RootModel) error {
	screen, err := newWindowsTCellScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		if tty, ok := screen.Tty(); ok {
			_ = tty.Close()
		}
		return fmt.Errorf("inicializar pantalla Tcell: %w", err)
	}
	screen.EnablePaste()
	screen.HideCursor()
	defer screen.Fini()

	frames := make(chan terminalFrame, 1)
	program := tea.NewProgram(
		headlessRoot{root: root, frames: frames},
		tea.WithoutRenderer(),
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutSignalHandler(),
	)
	programDone := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		programDone <- runErr
	}()

	events := make(chan tcell.Event, 64)
	eventQuit := make(chan struct{})
	var stopEvents sync.Once
	stopEventLoop := func() { stopEvents.Do(func() { close(eventQuit) }) }
	defer stopEventLoop()
	go screen.ChannelEvents(events, eventQuit)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	width, height := screen.Size()
	program.Send(tea.WindowSizeMsg{Width: width, Height: height})

	var input tcellInputState
	mouseCaptured := false
	forceSync := true
	previousOccupiedHeight := 0
	quitting := false

	for {
		select {
		case runErr := <-programDone:
			stopEventLoop()
			return runErr

		case <-signals:
			if !quitting {
				quitting = true
				program.Quit()
			}

		case frame := <-frames:
			if frame.captureMouse != mouseCaptured {
				mouseCaptured = frame.captureMouse
				if mouseCaptured {
					screen.EnableMouse(tcell.MouseMotionEvents)
				} else {
					screen.DisableMouse()
				}
			}
			occupiedHeight := renderTCell(
				screen,
				frame.view,
				previousOccupiedHeight,
				forceSync || frame.forceSync,
			)
			forceSync = false
			previousOccupiedHeight = occupiedHeight

		case event, ok := <-events:
			if !ok {
				if !quitting {
					quitting = true
					program.Quit()
				}
				continue
			}
			if _, resized := event.(*tcell.EventResize); resized {
				forceSync = true
			}
			for _, msg := range input.messages(event) {
				program.Send(msg)
			}
		}
	}
}
