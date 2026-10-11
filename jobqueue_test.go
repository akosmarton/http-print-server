package main

import (
	"sync"
	"testing"
)

func TestJobQueueClearDrainsInPlace(t *testing.T) {
	q := NewJobQueue(4)
	q.Init("p")
	ch := q.Get("p")
	ch <- Job{ContentType: "text/plain", Payload: []byte("a")}
	ch <- Job{ContentType: "text/plain", Payload: []byte("b")}
	q.Clear("p")
	if q.Get("p") != ch {
		t.Fatal("Clear must not replace the channel: blocked long-polls would be orphaned")
	}
	if len(ch) != 0 {
		t.Fatalf("Clear must drain queued jobs, len=%d", len(ch))
	}
}

func TestJobQueueGetUnknownPrinter(t *testing.T) {
	q := NewJobQueue(4)
	if q.Get("nope") != nil {
		t.Fatal("expected nil channel for unknown printer")
	}
}

// Regression for the concurrent map access crash (DELETE vs GET/POST).
func TestJobQueueConcurrentClearGet(t *testing.T) {
	q := NewJobQueue(4)
	q.Init("p")
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); _ = q.Get("p") }()
		go func() { defer wg.Done(); q.Clear("p") }()
	}
	wg.Wait()
}
