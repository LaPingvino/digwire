//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type AppLock struct {
	file *os.File
}

func AcquireAppLock(lockPath string) (*AppLock, error) {
	_ = os.MkdirAll(filepath.Dir(lockPath), 0755)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lockfile %s: %w", lockPath, err)
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock already held: %w", err)
	}
	// The pid lets a later start find the holder, and end it if it no longer answers.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	return &AppLock{file: f}, nil
}

// lockHolderPID is the process that wrote the lock file, or 0 if that cannot be told.
func lockHolderPID(lockPath string) int {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 || pid == os.Getpid() {
		return 0
	}
	if proc, err := os.FindProcess(pid); err != nil || proc.Signal(syscall.Signal(0)) != nil {
		return 0 // Gone already.
	}
	return pid
}

// TakeOverLock ends an instance that holds the lock but no longer serves the interface, and takes
// the lock over. A frozen instance must not keep the user locked out of their own downloads.
func TakeOverLock(lockPath string, wait time.Duration) (*AppLock, int, error) {
	pid := lockHolderPID(lockPath)
	if pid == 0 {
		// Nobody to end; the lock may just have been released.
		lock, err := AcquireAppLock(lockPath)
		return lock, 0, err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, pid, err
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		_ = proc.Signal(sig)
		deadline := time.Now().Add(wait)
		for time.Now().Before(deadline) {
			if lock, err := AcquireAppLock(lockPath); err == nil {
				return lock, pid, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return nil, pid, fmt.Errorf("instance %d keeps holding the lock", pid)
}

func (al *AppLock) Release() {
	if al != nil && al.file != nil {
		_ = syscall.Flock(int(al.file.Fd()), syscall.LOCK_UN)
		_ = al.file.Close()
	}
}
