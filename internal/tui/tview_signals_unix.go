//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyTViewSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

func stopTViewSignals(ch chan<- os.Signal) {
	signal.Stop(ch)
}
