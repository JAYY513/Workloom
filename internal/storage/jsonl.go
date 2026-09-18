package storage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// TailStatus describes whether a JSONL file can be appended to.
type TailStatus struct {
	// Size is the current file size in bytes (0 when the file is missing).
	Size int64
	// Complete is true when the file is empty or ends with a line terminator.
	// A false value marks a torn append left by an interrupted writer.
	Complete bool
}

// InspectJSONL reports the size and tail state of a JSONL file. A missing file
// is treated as a complete empty file.
func InspectJSONL(path string) (TailStatus, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return TailStatus{Size: 0, Complete: true}, nil
	}
	if err != nil {
		return TailStatus{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return TailStatus{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	if st.Size() == 0 {
		return TailStatus{Size: 0, Complete: true}, nil
	}
	if _, err := f.Seek(-1, io.SeekEnd); err != nil {
		return TailStatus{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	var last [1]byte
	if _, err := io.ReadFull(f, last[:]); err != nil {
		return TailStatus{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	return TailStatus{Size: st.Size(), Complete: last[0] == '\n'}, nil
}

// ScanJSONL calls fn for every complete line of a JSONL file, without the line
// terminator (a trailing CR is also stripped). A missing file scans as empty.
// A file whose last line has no terminator fails with ErrIncompleteTail before
// that line is handed to fn, so callers never observe a torn record.
func ScanJSONL(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)
	for n := 1; ; n++ {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			if raw[len(raw)-1] != '\n' {
				return fmt.Errorf("%w: %s: line %d", ErrIncompleteTail, path, n)
			}
			line := bytes.TrimRight(raw[:len(raw)-1], "\r")
			if err := fn(line); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
	}
}

// appendJSONL adds payload to path, requiring the file to be exactly expectSize
// bytes long and to end on a line boundary, then fsyncs. Callers must already
// hold the project lock. A size mismatch means a writer bypassed the lock and
// is reported instead of being papered over.
func appendJSONL(path string, payload []byte, expectSize int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent of %s: %w", path, err)
	}
	tail, err := InspectJSONL(path)
	if err != nil {
		return err
	}
	if !tail.Complete {
		return fmt.Errorf("%w: %s: %d bytes without line terminator", ErrIncompleteTail, path, tail.Size)
	}
	if tail.Size != expectSize {
		return fmt.Errorf("%s: size changed: %d bytes, expected %d", path, tail.Size, expectSize)
	}
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		return fmt.Errorf("append %s: payload is not a JSONL record", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}
