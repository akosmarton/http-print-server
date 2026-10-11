package main

import "sync"

type Job struct {
	ContentType string
	Payload     []byte
}

type JobQueue struct {
	mu       sync.RWMutex
	printers map[string]chan Job
	capacity int
}

func NewJobQueue(capacity int) *JobQueue {
	return &JobQueue{
		printers: make(map[string]chan Job),
		capacity: capacity,
	}
}

func (q *JobQueue) Get(printer string) chan Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.printers[printer]
}

func (q *JobQueue) Init(printer string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.printers[printer] = make(chan Job, q.capacity)
}

// Clear discards queued jobs but keeps the same channel so that
// long-poll consumers already blocked on it keep receiving new jobs.
func (q *JobQueue) Clear(printer string) {
	q.mu.RLock()
	ch, ok := q.printers[printer]
	q.mu.RUnlock()
	if !ok {
		return
	}
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
