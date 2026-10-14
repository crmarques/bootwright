package access

import (
	"bytes"
	"io"
	"sync"
)

const maxLineBytes = 64 << 10

type destination struct {
	mu sync.Mutex
	to io.Writer
}

func newDestination(to io.Writer) *destination {
	if to == nil {
		to = io.Discard
	}
	return &destination{to: to}
}

func (d *destination) write(line []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = d.to.Write(line)
}

func (d *destination) session(name string) *lineWriter {
	line := make([]byte, 0, len(name)+2+512)
	line = append(append(line, name...), ": "...)
	return &lineWriter{to: d, line: line, prefix: len(line)}
}

type lineWriter struct {
	mu     sync.Mutex
	to     *destination
	line   []byte
	prefix int
	split  bool
}

func (w *lineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := len(data)
	for len(data) > 0 {
		pending := len(w.line) - w.prefix
		segment := data[:min(len(data), maxLineBytes-pending)]
		if end := bytes.IndexByte(segment, '\n'); end >= 0 {
			if end == 0 && pending == 0 && w.split {
				w.split = false
			} else {
				w.line = append(w.line, segment[:end]...)
				w.flush()
			}
			data = data[end+1:]
			continue
		}
		w.line = append(w.line, segment...)
		data = data[len(segment):]
		w.split = false
		if len(w.line)-w.prefix == maxLineBytes {
			w.flush()
			w.split = true
		}
	}
	return written, nil
}

func (w *lineWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.line) > w.prefix {
		w.flush()
	}
	w.split = false
}

func (w *lineWriter) flush() {
	w.line = append(w.line, '\n')
	w.to.write(w.line)
	w.line = w.line[:w.prefix]
	w.split = false
}
