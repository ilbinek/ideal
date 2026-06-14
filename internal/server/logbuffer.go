package server

import "sync"

// logBuffer retains recent process stdout/stderr for live tailing in the UI.
type logBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newLogBuffer(maxBytes int) *logBuffer {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return &logBuffer{max: maxBytes}
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.max:]...)
	}
	return len(p), nil
}

func (b *logBuffer) ReadFrom(offset int) (string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if offset < 0 || offset > len(b.buf) {
		offset = 0
	}
	return string(b.buf[offset:]), len(b.buf)
}
