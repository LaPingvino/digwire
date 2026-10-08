//go:build !windows

package main

import (
	"os"
	"syscall"
)

// dumpSignals ask for a goroutine dump: SIGUSR1 is the usual way to prod a running process.
func dumpSignals() []os.Signal {
	return []os.Signal{syscall.SIGUSR1}
}
