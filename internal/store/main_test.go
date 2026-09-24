package store

import (
	"os"
	"testing"
)

// TestMain prefers a tmpfs temp dir when available. On hosts where /tmp lives
// on a slow (fsync-heavy) disk, SQLite setup dominates test runtime; /dev/shm
// keeps the suite fast.
func TestMain(m *testing.M) {
	if _, err := os.Stat("/dev/shm"); err == nil {
		if dir, err := os.MkdirTemp("/dev/shm", "jharness-store"); err == nil {
			_ = os.Setenv("TMPDIR", "/dev/shm")
			code := m.Run()
			_ = os.RemoveAll(dir)
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}
