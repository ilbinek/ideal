package server

import (
	"bytes"
	"os"
	"sync"
)

// sessionLogWriter mirrors process output to a session file and keeps a small
// in-memory tail for quick reads. Data is written to the file immediately and
// fsync'd on Close so stop/exit does not leave a truncated file.
type sessionLogWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  []byte
	max  int
	last []byte // last Write payload; used to drop stdout+stderr echoes
}

func newSessionLogWriter(file *os.File, maxBytes int) *sessionLogWriter {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return &sessionLogWriter{file: file, max: maxBytes}
}

func (w *sessionLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// os/exec may copy stdout/stderr into the same writer. Arma often emits the
	// same line on both fds; drop exact consecutive duplicates.
	if len(p) > 0 && bytes.Equal(p, w.last) {
		return len(p), nil
	}
	w.last = append(w.last[:0], p...)

	if w.file != nil {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
		// Flush to the kernel so live file tails see output promptly.
		// Full fsync is deferred to Close — Sync on every Write stalls the
		// stdout copy goroutine and can truncate capture on kill/stop.
	}

	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.max:]...)
	}
	return len(p), nil
}

func (w *sessionLogWriter) ReadFrom(offset int) (string, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if offset < 0 || offset > len(w.buf) {
		offset = 0
	}
	return string(w.buf[offset:]), len(w.buf)
}

func (w *sessionLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	_ = w.file.Sync()
	err := w.file.Close()
	w.file = nil
	return err
}
