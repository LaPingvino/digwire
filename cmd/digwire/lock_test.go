package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// An instance that holds the lock but no longer answers must not lock the user out: the next start
// ends it and takes the lock over.
func TestTakeOverLockFromUnresponsiveInstance(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "digwire.lock")

	// A stand-in for a frozen Digwire: it holds the lock and does nothing.
	holder := exec.Command("/bin/sh", "-c", "exec sleep 120")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Process.Kill() }()
	if err := os.WriteFile(lockPath, []byte(itoa(holder.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}

	lock, pid, err := TakeOverLock(lockPath, 5*time.Second)
	if err != nil {
		t.Fatalf("taking over: %v", err)
	}
	defer lock.Release()
	if pid != holder.Process.Pid {
		t.Fatalf("ended pid %d, want %d", pid, holder.Process.Pid)
	}
	if err := holder.Wait(); err == nil {
		t.Fatal("the unresponsive instance is still running")
	}
	if got := lockHolderPID(lockPath); got != 0 {
		t.Fatalf("lock still names pid %d after takeover", got)
	}
}

// A lock left behind by a process that no longer exists is simply taken.
func TestTakeOverStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "digwire.lock")
	if err := os.WriteFile(lockPath, []byte("999999"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, pid, err := TakeOverLock(lockPath, time.Second)
	if err != nil {
		t.Fatalf("taking over a stale lock: %v", err)
	}
	defer lock.Release()
	if pid != 0 {
		t.Fatalf("reported ending pid %d for a stale lock", pid)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
