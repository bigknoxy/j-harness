package engine

import (
	"os"
	"testing"
)

// TestMain prefers a tmpfs temp dir when available (SQLite setup on a slow,
// fsync-heavy disk otherwise dominates the suite).
func TestMain(m *testing.M) {
	if _, err := os.Stat("/dev/shm"); err == nil {
		if dir, err := os.MkdirTemp("/dev/shm", "jharness-engine"); err == nil {
			_ = os.Setenv("TMPDIR", "/dev/shm")
			code := m.Run()
			_ = os.RemoveAll(dir)
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}
