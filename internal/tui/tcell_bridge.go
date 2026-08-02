package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/lilith/li/internal/tui/uikit"
)

// tcellInputState translates Tcell events into the Lilith uikit messages used
// by Lilith's existing models. Bracketed paste is accumulated and emitted as a
// single KeyMsg so a large clipboard never becomes thousands of renders.
type tcellInputState struct {
	pasting          bool
	paste            strings.Builder
	pasteEndedWithCR bool
	lastButton       tcell.ButtonMask
}

// bridgeTCellEvents drains the physical Tcell stream independently from the
// model/render loop. This is important on Windows: a bracketed paste is
// delivered as one EventKey per rune, and rendering a concurrent streaming
// frame must not stop us from consuming those events. The bridge accumulates
// the complete paste before publishing a single uikit.KeyMsg downstream.
func bridgeTCellEvents(events <-chan tcell.Event, messages chan<- uikit.Msg, quit <-chan struct{}) {
	defer close(messages)

	var input tcellInputState
	for {
		select {
		case <-quit:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			for _, msg := range input.messages(event) {
				select {
				case messages <- msg:
				case <-quit:
					return
				}
			}
		}
	}
}

func (s *tcellInputState) messages(event tcell.Event) []uikit.Msg {
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
		return []uikit.Msg{uikit.KeyMsg{
			Type:  uikit.KeyRunes,
			Runes: []rune(text),
			Paste: true,
		}}

	case *tcell.EventKey:
		if s.pasting {
			s.appendPasteKey(event)
			return nil
		}
		if msg, ok := tcellKeyMsg(event); ok {
			return []uikit.Msg{msg}
		}

	case *tcell.EventResize:
		width, height := event.Size()
		return []uikit.Msg{uikit.WindowSizeMsg{Width: width, Height: height}}

	case *tcell.EventMouse:
		if msg, ok := s.mouseMsg(event); ok {
			return []uikit.Msg{msg}
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

func tcellKeyMsg(event *tcell.EventKey) (uikit.KeyMsg, bool) {
	key := event.Key()
	modifiers := event.Modifiers()
	alt := modifiers&(tcell.ModAlt|tcell.ModMeta) != 0
	ctrl := modifiers&tcell.ModCtrl != 0
	shift := modifiers&tcell.ModShift != 0

	// Many SSH/PTY combinations report the Return key as Ctrl+M (CR) instead
	// of KeyEnter. Treat both as the same physical Enter key. Ctrl+M has no
	// separate Lilith shortcut, and leaving it as a control key made submissions
	// appear to be ignored on some VPS terminals.
	if key == tcell.KeyEnter || key == tcell.KeyCtrlM {
		return uikit.KeyMsg{Type: uikit.KeyEnter, Alt: alt}, true
	}

	if key == tcell.KeyRune {
		r := event.Rune()
		if ctrl {
			if keyType, ok := controlRuneKey(r); ok {
				return uikit.KeyMsg{Type: keyType, Alt: alt}, true
			}
		}
		if r == ' ' {
			// Preserve the rune because the internal textarea inserts msg.Runes.
			return uikit.KeyMsg{Type: uikit.KeySpace, Runes: []rune{' '}, Alt: alt}, true
		}
		if r == 0 {
			return uikit.KeyMsg{}, false
		}
		return uikit.KeyMsg{Type: uikit.KeyRunes, Runes: []rune{r}, Alt: alt}, true
	}

	if keyType, ok := tcellControlKey(key); ok {
		return uikit.KeyMsg{Type: keyType, Alt: alt}, true
	}

	switch key {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return uikit.KeyMsg{Type: uikit.KeyBackspace, Alt: alt}, true
	case tcell.KeyTab:
		return uikit.KeyMsg{Type: uikit.KeyTab, Alt: alt}, true
	case tcell.KeyBacktab:
		return uikit.KeyMsg{Type: uikit.KeyShiftTab, Alt: alt}, true
	case tcell.KeyEsc:
		return uikit.KeyMsg{Type: uikit.KeyEsc, Alt: alt}, true
	case tcell.KeyDelete:
		return uikit.KeyMsg{Type: uikit.KeyDelete, Alt: alt}, true
	case tcell.KeyInsert:
		return uikit.KeyMsg{Type: uikit.KeyInsert, Alt: alt}, true
	case tcell.KeyPgUp:
		if ctrl {
			return uikit.KeyMsg{Type: uikit.KeyCtrlPgUp, Alt: alt}, true
		}
		return uikit.KeyMsg{Type: uikit.KeyPgUp, Alt: alt}, true
	case tcell.KeyPgDn:
		if ctrl {
			return uikit.KeyMsg{Type: uikit.KeyCtrlPgDown, Alt: alt}, true
		}
		return uikit.KeyMsg{Type: uikit.KeyPgDown, Alt: alt}, true
	case tcell.KeyUp:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyUp, uikit.KeyCtrlUp, uikit.KeyShiftUp, uikit.KeyCtrlShiftUp, ctrl, shift), Alt: alt}, true
	case tcell.KeyDown:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyDown, uikit.KeyCtrlDown, uikit.KeyShiftDown, uikit.KeyCtrlShiftDown, ctrl, shift), Alt: alt}, true
	case tcell.KeyLeft:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyLeft, uikit.KeyCtrlLeft, uikit.KeyShiftLeft, uikit.KeyCtrlShiftLeft, ctrl, shift), Alt: alt}, true
	case tcell.KeyRight:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyRight, uikit.KeyCtrlRight, uikit.KeyShiftRight, uikit.KeyCtrlShiftRight, ctrl, shift), Alt: alt}, true
	case tcell.KeyHome:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyHome, uikit.KeyCtrlHome, uikit.KeyShiftHome, uikit.KeyCtrlShiftHome, ctrl, shift), Alt: alt}, true
	case tcell.KeyEnd:
		return uikit.KeyMsg{Type: directionalKey(uikit.KeyEnd, uikit.KeyCtrlEnd, uikit.KeyShiftEnd, uikit.KeyCtrlShiftEnd, ctrl, shift), Alt: alt}, true
	}

	if key >= tcell.KeyF1 && key <= tcell.KeyF20 {
		return uikit.KeyMsg{
			Type: uikit.KeyType(int(uikit.KeyF1) + int(key-tcell.KeyF1)),
			Alt:  alt,
		}, true
	}

	return uikit.KeyMsg{}, false
}

func tcellControlKey(key tcell.Key) (uikit.KeyType, bool) {
	switch key {
	case tcell.KeyCtrlSpace:
		return uikit.KeyCtrlAt, true
	case tcell.KeyCtrlA:
		return uikit.KeyCtrlA, true
	case tcell.KeyCtrlB:
		return uikit.KeyCtrlB, true
	case tcell.KeyCtrlC:
		return uikit.KeyCtrlC, true
	case tcell.KeyCtrlD:
		return uikit.KeyCtrlD, true
	case tcell.KeyCtrlE:
		return uikit.KeyCtrlE, true
	case tcell.KeyCtrlF:
		return uikit.KeyCtrlF, true
	case tcell.KeyCtrlG:
		return uikit.KeyCtrlG, true
	case tcell.KeyCtrlH:
		return uikit.KeyCtrlH, true
	case tcell.KeyCtrlI:
		return uikit.KeyCtrlI, true
	case tcell.KeyCtrlJ:
		return uikit.KeyCtrlJ, true
	case tcell.KeyCtrlK:
		return uikit.KeyCtrlK, true
	case tcell.KeyCtrlL:
		return uikit.KeyCtrlL, true
	case tcell.KeyCtrlM:
		return uikit.KeyCtrlM, true
	case tcell.KeyCtrlN:
		return uikit.KeyCtrlN, true
	case tcell.KeyCtrlO:
		return uikit.KeyCtrlO, true
	case tcell.KeyCtrlP:
		return uikit.KeyCtrlP, true
	case tcell.KeyCtrlQ:
		return uikit.KeyCtrlQ, true
	case tcell.KeyCtrlR:
		return uikit.KeyCtrlR, true
	case tcell.KeyCtrlS:
		return uikit.KeyCtrlS, true
	case tcell.KeyCtrlT:
		return uikit.KeyCtrlT, true
	case tcell.KeyCtrlU:
		return uikit.KeyCtrlU, true
	case tcell.KeyCtrlV:
		return uikit.KeyCtrlV, true
	case tcell.KeyCtrlW:
		return uikit.KeyCtrlW, true
	case tcell.KeyCtrlX:
		return uikit.KeyCtrlX, true
	case tcell.KeyCtrlY:
		return uikit.KeyCtrlY, true
	case tcell.KeyCtrlZ:
		return uikit.KeyCtrlZ, true
	case tcell.KeyCtrlLeftSq:
		return uikit.KeyCtrlOpenBracket, true
	case tcell.KeyCtrlBackslash:
		return uikit.KeyCtrlBackslash, true
	case tcell.KeyCtrlRightSq:
		return uikit.KeyCtrlCloseBracket, true
	case tcell.KeyCtrlCarat:
		return uikit.KeyCtrlCaret, true
	case tcell.KeyCtrlUnderscore:
		return uikit.KeyCtrlUnderscore, true
	default:
		return 0, false
	}
}

func controlRuneKey(r rune) (uikit.KeyType, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return uikit.KeyType(r-'a') + uikit.KeyCtrlA, true
	case r >= 'A' && r <= 'Z':
		return uikit.KeyType(r-'A') + uikit.KeyCtrlA, true
	case r == ' ' || r == '@':
		return uikit.KeyCtrlAt, true
	case r == '[':
		return uikit.KeyCtrlOpenBracket, true
	case r == '\\':
		return uikit.KeyCtrlBackslash, true
	case r == ']':
		return uikit.KeyCtrlCloseBracket, true
	case r == '^':
		return uikit.KeyCtrlCaret, true
	case r == '_':
		return uikit.KeyCtrlUnderscore, true
	default:
		return 0, false
	}
}

func directionalKey(normal, control, shifted, controlShift uikit.KeyType, ctrl, shift bool) uikit.KeyType {
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

func (s *tcellInputState) mouseMsg(event *tcell.EventMouse) (uikit.MouseMsg, bool) {
	x, y := event.Position()
	buttons := event.Buttons()
	modifiers := event.Modifiers()
	base := uikit.MouseEvent{
		X:     x,
		Y:     y,
		Shift: modifiers&tcell.ModShift != 0,
		Alt:   modifiers&(tcell.ModAlt|tcell.ModMeta) != 0,
		Ctrl:  modifiers&tcell.ModCtrl != 0,
	}

	if button, ok := wheelButton(buttons); ok {
		base.Action = uikit.MouseActionPress
		base.Button = button
		return uikit.MouseMsg(base), true
	}

	current := physicalButtonMask(buttons)
	switch {
	case current == tcell.ButtonNone && s.lastButton != tcell.ButtonNone:
		base.Action = uikit.MouseActionRelease
		base.Button = bubbleMouseButton(s.lastButton)
		s.lastButton = tcell.ButtonNone
		return uikit.MouseMsg(base), true

	case current != tcell.ButtonNone && s.lastButton == tcell.ButtonNone:
		s.lastButton = current
		base.Action = uikit.MouseActionPress
		base.Button = bubbleMouseButton(current)
		return uikit.MouseMsg(base), true

	case current != tcell.ButtonNone:
		s.lastButton = current
		base.Action = uikit.MouseActionMotion
		base.Button = bubbleMouseButton(current)
		return uikit.MouseMsg(base), true

	default:
		base.Action = uikit.MouseActionMotion
		base.Button = uikit.MouseButtonNone
		return uikit.MouseMsg(base), true
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

func wheelButton(buttons tcell.ButtonMask) (uikit.MouseButton, bool) {
	switch {
	case buttons&tcell.WheelUp != 0:
		return uikit.MouseButtonWheelUp, true
	case buttons&tcell.WheelDown != 0:
		return uikit.MouseButtonWheelDown, true
	case buttons&tcell.WheelLeft != 0:
		return uikit.MouseButtonWheelLeft, true
	case buttons&tcell.WheelRight != 0:
		return uikit.MouseButtonWheelRight, true
	default:
		return uikit.MouseButtonNone, false
	}
}

func bubbleMouseButton(button tcell.ButtonMask) uikit.MouseButton {
	switch button {
	case tcell.Button1:
		return uikit.MouseButtonLeft
	case tcell.Button2:
		return uikit.MouseButtonRight
	case tcell.Button3:
		return uikit.MouseButtonMiddle
	case tcell.Button4:
		return uikit.MouseButtonForward
	case tcell.Button5:
		return uikit.MouseButtonBackward
	case tcell.Button6:
		return uikit.MouseButton10
	case tcell.Button7, tcell.Button8:
		return uikit.MouseButton11
	default:
		return uikit.MouseButtonNone
	}
}
