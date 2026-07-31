//go:build !windows

package tui

// RunRoot starts Lilith with tview's native application runtime. tview creates
// and owns the platform Tcell screen, input loop, paste mode, and frame redraws.
func RunRoot(root RootModel) error {
	return runRootTView(root, nil)
}
