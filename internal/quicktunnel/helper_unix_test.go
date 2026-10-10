//go:build !windows

package quicktunnel

import (
	"os"
	"os/signal"
	"syscall"
)

func waitForTermination(ignore bool) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	for range signals {
		if !ignore {
			return
		}
	}
}

func waitForTerminationAndMark(path string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	_ = os.WriteFile(path+".ready", []byte("ready"), 0o600)
	<-signals
	_ = os.WriteFile(path, []byte("stopped"), 0o600)
}
