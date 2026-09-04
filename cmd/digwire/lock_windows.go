//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock = 0x00000002
	lockfileFailImmediate = 0x00000001
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
	var overlapped syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediate),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("instance lock already held: %w", err)
	}
	return &AppLock{file: f}, nil
}

func (al *AppLock) Release() {
	if al != nil && al.file != nil {
		var overlapped syscall.Overlapped
		_, _, _ = procUnlockFileEx.Call(
			al.file.Fd(),
			0,
			1,
			0,
			uintptr(unsafe.Pointer(&overlapped)),
		)
		_ = al.file.Close()
	}
}
