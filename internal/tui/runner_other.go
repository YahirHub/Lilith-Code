//go:build !windows

package tui

import tea "github.com/charmbracelet/bubbletea"

// RunRoot starts Lilith with Bubble Tea's native terminal runtime on
// non-Windows systems.
func RunRoot(root RootModel) error {
	program := tea.NewProgram(
		root,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := program.Run()
	return err
}
