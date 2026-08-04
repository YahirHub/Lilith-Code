package tui

import "testing"

func TestNewChatKeepsSinglePointerInstance(t *testing.T) {
	ctx := &AppContext{ConfigDir: t.TempDir(), Styles: NewStyles(DefaultTheme())}
	chat := NewChat(ctx)
	if chat == nil {
		t.Fatal("NewChat returned nil")
	}

	root := NewRootModel(ctx)
	if root.chat == nil {
		t.Fatal("NewRootModel did not retain the chat instance")
	}
	if root.current != root.chat {
		t.Fatal("the default screen must reference the same persistent ChatModel pointer")
	}
}
