//go:build windows

package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

const windowsTCellTerm = "xterm-256color"

// preinitializedTCellScreen prevents tview.SetScreen from initializing the
// explicit Windows screen a second time. RunRoot performs the first Init so it
// can report failures and close the console handle deterministically.
type preinitializedTCellScreen struct {
	tcell.Screen
}

func (s *preinitializedTCellScreen) Init() error { return nil }

// newWindowsTCellScreen deliberately selects Tcell's terminfo/VT backend.
// tview owns this screen after RunRoot passes it to Application.SetScreen.
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

// RunRoot starts the tview runtime using the explicit Windows VT screen so
// bracketed paste and Unicode input remain consistent in Terminal, PowerShell,
// and CMD.
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
	return runRootTView(root, &preinitializedTCellScreen{Screen: screen})
}
