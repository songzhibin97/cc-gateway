package sse

import (
	"fmt"
	"net/http"
)

// Writer writes SSE events to an http.ResponseWriter.
type Writer struct {
	w  http.ResponseWriter
	rc *http.ResponseController
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

func (w *Writer) flush() {
	_ = w.rc.Flush()
}

// WriteEvent writes a single SSE event with the given type and data.
func (w *Writer) WriteEvent(eventType string, data []byte) error {
	if eventType != "" {
		if _, err := fmt.Fprintf(w.w, "event: %s\n", eventType); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w.w, "data: %s\n\n", data); err != nil {
		return err
	}
	w.flush()
	return nil
}

// WriteRawEvent writes a pre-formatted SSE event.
func (w *Writer) WriteRawEvent(raw []byte) error {
	if _, err := w.w.Write(raw); err != nil {
		return err
	}
	w.flush()
	return nil
}
