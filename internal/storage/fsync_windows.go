//go:build windows

package storage

// fsyncDir is a no-op on Windows: directories cannot be opened for flushing.
// File contents are still fsynced (FlushFileBuffers) by the callers; no
// power-loss guarantee is claimed either way.
func fsyncDir(string) error { return nil }
