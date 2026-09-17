package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// AtomicWrite replaces path with data by writing a temporary file in the same
// directory, fsyncing it and renaming it over the destination. Readers observe
// either the old or the new content, never a partial file (方案 §15.2 单文件
// 原子写). Existing permissions are preserved when the file already exists,
// otherwise perm is used. The parent directory is fsynced on POSIX on a
// best-effort basis; that error is not propagated, and no power-loss guarantee
// is claimed.
func AtomicWrite(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	discard := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		discard()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		discard()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := renameWithRetry(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	_ = fsyncDir(dir) // best effort; directory fsync is unsupported on Windows
	return nil
}

// renameWithRetry renames oldPath over newPath, retrying briefly on Windows
// where a rename fails while another handle holds the destination open
// (verified behaviour: "Access is denied" because Go's os.Open omits
// FILE_SHARE_DELETE). POSIX renames do not need retries.
func renameWithRetry(oldPath, newPath string) error {
	const attempts = 5
	var err error
	for i := range attempts {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		if !transientRenameError(err) {
			break
		}
		time.Sleep(time.Duration(20*(i+1)) * time.Millisecond)
	}
	return fmt.Errorf("replace %s: %w", newPath, err)
}

// WriteFileSync writes data to path, creating it if needed, and fsyncs it.
// It is the primitive for new files (journal, payload) that are not replacing
// an existing version.
func WriteFileSync(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// readFileMaybe returns the content of path; a missing file yields exists=false
// instead of an error.
func readFileMaybe(path string) (data []byte, exists bool, err error) {
	data, err = os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}
