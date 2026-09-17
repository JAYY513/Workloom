//go:build !windows

package storage

import "os"

// fsyncDir flushes a directory entry update (POSIX). It is best effort: callers
// do not fail when the file system does not support it.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
