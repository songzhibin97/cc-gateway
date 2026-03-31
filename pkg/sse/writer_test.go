package sse

import (
	"net/http/httptest"
	"testing"
)

func TestBufferedWriterDefersPreludeUntilThreshold(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := NewBufferedWriter(rec, 100)

	if err := writer.WriteRawEvent([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")); err != nil {
		t.Fatalf("WriteRawEvent(message_start) returned error: %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no bytes written before threshold, got %d", rec.Body.Len())
	}

	if err := writer.WriteRawEvent([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\"}\n\n")); err != nil {
		t.Fatalf("WriteRawEvent(content_block_start) returned error: %v", err)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected buffered prelude to flush after threshold")
	}
}

func TestBufferedWriterFlushBufferedWritesTail(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := NewBufferedWriter(rec, 1024)

	if err := writer.WriteEvent("message_start", []byte(`{"type":"message_start"}`)); err != nil {
		t.Fatalf("WriteEvent returned error: %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no bytes written before explicit flush, got %d", rec.Body.Len())
	}

	if err := writer.FlushBuffered(); err != nil {
		t.Fatalf("FlushBuffered returned error: %v", err)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected FlushBuffered to write buffered bytes")
	}
}
