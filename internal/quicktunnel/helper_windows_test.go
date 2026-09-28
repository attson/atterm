//go:build windows

package quicktunnel

import "time"

func waitForTermination(ignore bool) {
	// taskkill owns graceful/forced process-tree termination on Windows.
	for {
		time.Sleep(time.Hour)
	}
}

func waitForTerminationAndMark(path string) {
	waitForTermination(false)
}
