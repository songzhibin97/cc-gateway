package sse

import (
	"bufio"
	"io"
	"strings"
)

// Event represents a single SSE event.
type Event struct {
	Type string // from "event:" line, empty if not present
	Data string // from "data:" line(s), joined by newline if multiple
}

// Reader reads SSE events from an io.Reader.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a new SSE reader.
func NewReader(r io.Reader) *Reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Reader{scanner: s}
}

// Next reads the next SSE event. Returns io.EOF when the stream ends.
func (r *Reader) Next() (Event, error) {
	var evt Event
	var dataLines []string

	for r.scanner.Scan() {
		line := r.scanner.Text()

		if strings.HasPrefix(line, ":") {
			continue
		}

		if line == "" {
			if len(dataLines) > 0 {
				evt.Data = strings.Join(dataLines, "\n")
				return evt, nil
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			evt.Type = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}

	if len(dataLines) > 0 {
		evt.Data = strings.Join(dataLines, "\n")
		return evt, nil
	}

	return Event{}, io.EOF
}
