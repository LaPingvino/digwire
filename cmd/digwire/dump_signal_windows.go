//go:build windows

package main

import "os"

// dumpSignals is empty on Windows, which has no user-defined signals. The dump still happens by
// itself when the engine stops responding.
func dumpSignals() []os.Signal {
	return nil
}
