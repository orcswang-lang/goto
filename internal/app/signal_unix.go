//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"
)

func signalNotifyWindowChange(ch chan<- struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			ch <- struct{}{}
		}
	}()
}
