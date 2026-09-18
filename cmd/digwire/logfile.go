package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// maxLogBytes is when the log is rolled over to .1; a stack trace is worth keeping, a year of
// startup lines is not.
const maxLogBytes = 4 << 20

// setupLogFile also writes the log to a file, so panics and errors survive being started from a
// desktop launcher, where stderr goes nowhere.
func setupLogFile() *os.File {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, "digwire")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil
	}
	path := filepath.Join(dir, "digwire.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return f
}
