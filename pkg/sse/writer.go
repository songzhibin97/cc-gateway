package sse

import (
	"bytes"
	"fmt"
	"net/http"
)

// Writer writes SSE events to an http.ResponseWriter.
type Writer struct {
	w             http.ResponseWriter
	rc            *http.ResponseController
	buffer        bytes.Buffer
	bufferLimit   int
	bufferEnabled bool
}

// NewWriter creates a new SSE writer.
func NewWriter(w http.ResponseWriter) *Writer {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	return &Writer{
		w:  w,
		rc: http.NewResponseController(w),
	}
}

// NewBufferedWriter creates a writer that buffers the initial SSE prelude until
// the buffered payload reaches the given byte threshold or FlushBuffered is
// called explicitly.
func NewBufferedWriter(w http.ResponseWriter, bufferLimit int) *Writer {
	writer := NewWriter(w)
	if bufferLimit > 0 {
		writer.bufferEnabled = true
		writer.bufferLimit = bufferLimit
	}
	return writer
}

func (w *Writer) flush() {
	_ = w.rc.Flush()
}

// FlushBuffered flushes any buffered SSE bytes to the underlying writer and
// disables further buffering.
func (w *Writer) FlushBuffered() error {
	if !w.bufferEnabled {
		return nil
	}

	if w.buffer.Len() > 0 {
		if _, err := w.w.Write(w.buffer.Bytes()); err != nil {
			return err
		}
		w.flush()
		w.buffer.Reset()
	}

	w.bufferEnabled = false
	return nil
}

func (w *Writer) write(raw []byte) error {
	if w.bufferEnabled {
		if _, err := w.buffer.Write(raw); err != nil {
			return err
		}
		if w.buffer.Len() >= w.bufferLimit {
			return w.FlushBuffered()
		}
		return nil
	}

	if _, err := w.w.Write(raw); err != nil {
		return err
	}
	w.flush()
	return nil
}

// WriteEvent writes a single SSE event with the given type and data.
func (w *Writer) WriteEvent(eventType string, data []byte) error {
	var raw bytes.Buffer
	if eventType != "" {
		if _, err := fmt.Fprintf(&raw, "event: %s\n", eventType); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(&raw, "data: %s\n\n", data); err != nil {
		return err
	}
	return w.write(raw.Bytes())
}

// WriteRawEvent writes a pre-formatted SSE event.
func (w *Writer) WriteRawEvent(raw []byte) error {
	return w.write(raw)
}
