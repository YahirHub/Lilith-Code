// Package uikit defines Lilith's framework-neutral event and command model.
// tview owns the physical terminal; these small types keep the agent state
// machine independent from any particular terminal UI library.
package uikit

import (
	"strings"
	"time"
)

type Msg any

type Cmd func() Msg

type Model interface {
	Init() Cmd
	Update(Msg) (Model, Cmd)
	View() string
}

type BatchMsg []Cmd

type QuitMsg struct{}
type WindowSizeMsg struct{ Width, Height int }
type mouseModeMsg struct{ Enabled bool }

func Batch(cmds ...Cmd) Cmd {
	filtered := make([]Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func() Msg { return BatchMsg(filtered) }
}

func Tick(d time.Duration, fn func(time.Time) Msg) Cmd {
	if fn == nil {
		return nil
	}
	return func() Msg {
		t := time.NewTimer(d)
		defer t.Stop()
		return fn(<-t.C)
	}
}

func WindowSize() Cmd { return nil }

var (
	Quit                  Cmd = func() Msg { return QuitMsg{} }
	EnableMouseCellMotion Cmd = func() Msg { return mouseModeMsg{Enabled: true} }
	DisableMouse          Cmd = func() Msg { return mouseModeMsg{Enabled: false} }
)

type KeyType int

const (
	KeyUnknown KeyType = iota
	KeyRunes
	KeySpace
	KeyEnter
	KeyBackspace
	KeyTab
	KeyShiftTab
	KeyEsc
	KeyDelete
	KeyInsert
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDown
	KeyCtrlPgUp
	KeyCtrlPgDown
	KeyShiftUp
	KeyShiftDown
	KeyShiftLeft
	KeyShiftRight
	KeyShiftHome
	KeyShiftEnd
	KeyCtrlUp
	KeyCtrlDown
	KeyCtrlLeft
	KeyCtrlRight
	KeyCtrlHome
	KeyCtrlEnd
	KeyCtrlShiftUp
	KeyCtrlShiftDown
	KeyCtrlShiftLeft
	KeyCtrlShiftRight
	KeyCtrlShiftHome
	KeyCtrlShiftEnd
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	KeyCtrlAt
	KeyCtrlA
	KeyCtrlB
	KeyCtrlC
	KeyCtrlD
	KeyCtrlE
	KeyCtrlF
	KeyCtrlG
	KeyCtrlH
	KeyCtrlI
	KeyCtrlJ
	KeyCtrlK
	KeyCtrlL
	KeyCtrlM
	KeyCtrlN
	KeyCtrlO
	KeyCtrlP
	KeyCtrlQ
	KeyCtrlR
	KeyCtrlS
	KeyCtrlT
	KeyCtrlU
	KeyCtrlV
	KeyCtrlW
	KeyCtrlX
	KeyCtrlY
	KeyCtrlZ
	KeyCtrlOpenBracket
	KeyCtrlBackslash
	KeyCtrlCloseBracket
	KeyCtrlCaret
	KeyCtrlUnderscore
)

type KeyMsg struct {
	Type  KeyType
	Runes []rune
	Alt   bool
	Paste bool
}

func (k KeyMsg) String() string {
	var value string
	switch k.Type {
	case KeyRunes:
		value = string(k.Runes)
	case KeySpace:
		value = " "
	case KeyEnter:
		value = "enter"
	case KeyBackspace:
		value = "backspace"
	case KeyTab:
		value = "tab"
	case KeyShiftTab:
		value = "shift+tab"
	case KeyEsc:
		value = "esc"
	case KeyDelete:
		value = "delete"
	case KeyInsert:
		value = "insert"
	case KeyUp:
		value = "up"
	case KeyDown:
		value = "down"
	case KeyLeft:
		value = "left"
	case KeyRight:
		value = "right"
	case KeyHome:
		value = "home"
	case KeyEnd:
		value = "end"
	case KeyPgUp:
		value = "pgup"
	case KeyPgDown:
		value = "pgdown"
	case KeyCtrlPgUp:
		value = "ctrl+pgup"
	case KeyCtrlPgDown:
		value = "ctrl+pgdown"
	case KeyShiftUp:
		value = "shift+up"
	case KeyShiftDown:
		value = "shift+down"
	case KeyShiftLeft:
		value = "shift+left"
	case KeyShiftRight:
		value = "shift+right"
	case KeyShiftHome:
		value = "shift+home"
	case KeyShiftEnd:
		value = "shift+end"
	case KeyCtrlUp:
		value = "ctrl+up"
	case KeyCtrlDown:
		value = "ctrl+down"
	case KeyCtrlLeft:
		value = "ctrl+left"
	case KeyCtrlRight:
		value = "ctrl+right"
	case KeyCtrlHome:
		value = "ctrl+home"
	case KeyCtrlEnd:
		value = "ctrl+end"
	case KeyCtrlShiftUp:
		value = "ctrl+shift+up"
	case KeyCtrlShiftDown:
		value = "ctrl+shift+down"
	case KeyCtrlShiftLeft:
		value = "ctrl+shift+left"
	case KeyCtrlShiftRight:
		value = "ctrl+shift+right"
	case KeyCtrlShiftHome:
		value = "ctrl+shift+home"
	case KeyCtrlShiftEnd:
		value = "ctrl+shift+end"
	case KeyCtrlAt:
		value = "ctrl+@"
	case KeyCtrlOpenBracket:
		value = "ctrl+["
	case KeyCtrlBackslash:
		value = "ctrl+\\"
	case KeyCtrlCloseBracket:
		value = "ctrl+]"
	case KeyCtrlCaret:
		value = "ctrl+^"
	case KeyCtrlUnderscore:
		value = "ctrl+_"
	default:
		if k.Type >= KeyF1 && k.Type <= KeyF20 {
			value = "f" + itoa(int(k.Type-KeyF1)+1)
		} else if k.Type >= KeyCtrlA && k.Type <= KeyCtrlZ {
			value = "ctrl+" + string(rune('a'+k.Type-KeyCtrlA))
		}
	}
	if k.Alt && value != "" && !strings.HasPrefix(value, "alt+") {
		value = "alt+" + value
	}
	return value
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

type MouseAction int

const (
	MouseActionPress MouseAction = iota
	MouseActionRelease
	MouseActionMotion
)

type MouseButton int

const (
	MouseButtonNone MouseButton = iota
	MouseButtonLeft
	MouseButtonMiddle
	MouseButtonRight
	MouseButtonWheelUp
	MouseButtonWheelDown
	MouseButtonWheelLeft
	MouseButtonWheelRight
	MouseButtonForward
	MouseButtonBackward
	MouseButton10
	MouseButton11
)

type MouseEvent struct {
	X, Y   int
	Shift  bool
	Alt    bool
	Ctrl   bool
	Action MouseAction
	Button MouseButton
}

type MouseMsg MouseEvent

func (m MouseEvent) IsWheel() bool {
	switch m.Button {
	case MouseButtonWheelUp, MouseButtonWheelDown, MouseButtonWheelLeft, MouseButtonWheelRight:
		return true
	default:
		return false
	}
}

func (m MouseMsg) IsWheel() bool { return MouseEvent(m).IsWheel() }
