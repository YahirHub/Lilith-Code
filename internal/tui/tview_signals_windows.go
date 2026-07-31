//go:build windows

package tui

import (
	"os"
	"os/signal"
)

func notifyTViewSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}

func stopTViewSignals(ch chan<- os.Signal) {
	signal.Stop(ch)
}
