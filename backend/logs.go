package main

import (
	"sync"
)

const logLimit = 64 * 1024

// A single fixed-size tail buffer. The OS pipe is drained regardless of whether
// anyone opens the UI. Flushes go only to the volatile runtime directory.
type tailLog struct {
	mu    sync.Mutex
	b     []byte
	dirty bool
}

func (l *tailLog) Write(p []byte) (int, error) {
	n := len(p)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(p) >= logLimit {
		l.b = append(l.b[:0], p[len(p)-logLimit:]...)
	} else {
		over := len(l.b) + len(p) - logLimit
		if over > 0 {
			copy(l.b, l.b[over:])
			l.b = l.b[:len(l.b)-over]
		}
		l.b = append(l.b, p...)
	}
	l.dirty = true
	return n, nil
}
func (l *tailLog) snapshot() string { l.mu.Lock(); defer l.mu.Unlock(); return string(l.b) }
func (l *tailLog) flush(path string) error {
	l.mu.Lock()
	if !l.dirty {
		l.mu.Unlock()
		return nil
	}
	b := append([]byte(nil), l.b...)
	l.dirty = false
	l.mu.Unlock()
	if err := atomicBytes(path, b); err != nil {
		l.mu.Lock()
		l.dirty = true
		l.mu.Unlock()
		return err
	}
	return nil
}
