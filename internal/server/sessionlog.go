package server

import (
	"os"
	"sync"
)

// sessionLogWriter mirrors process output to a session file (flushed immediately)
// and keeps a small in-memory tail for quick reads.
type sessionLogWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  []byte
	max  int
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

	if w.file != nil {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
		_ = w.file.Sync()
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
	err := w.file.Close()
	w.file = nil
	return err
}
