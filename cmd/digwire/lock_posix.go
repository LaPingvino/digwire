//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
	return &AppLock{file: f}, nil
}

func (al *AppLock) Release() {
	if al != nil && al.file != nil {
		_ = syscall.Flock(int(al.file.Fd()), syscall.LOCK_UN)
		_ = al.file.Close()
	}
}
