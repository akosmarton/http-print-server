package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newQueue replaces the package-global queue. Tests must not use t.Parallel().
func newQueue(t *testing.T, capacity int, printers ...string) {
	t.Helper()
	jobQueue = NewJobQueue(capacity)
	for _, p := range printers {
		jobQueue.Init(p)
	}
}

func newContext(t *testing.T, method, printer string, body io.Reader, contentType string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, "/", body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("printer")
	c.SetParamValues(printer)
	return c, rec
}

func call(t *testing.T, h echo.HandlerFunc, c echo.Context) {
	t.Helper()
	if err := h(c); err != nil {
		c.Echo().DefaultHTTPErrorHandler(err, c)
	}
}

func TestGetJobUnknownPrinter(t *testing.T) {
	newQueue(t, 4, "p")
	c, rec := newContext(t, "GET", "nope", nil, "")
	call(t, getJob, c)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostJobUnknownPrinter(t *testing.T) {
	newQueue(t, 4, "p")
	c, rec := newContext(t, "POST", "nope", strings.NewReader("hello"), "text/plain")
	call(t, postJob, c)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostJobCreated(t *testing.T) {
	newQueue(t, 4, "p")
	c, rec := newContext(t, "POST", "p", strings.NewReader("hello"), "text/plain")
	call(t, postJob, c)
	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if n := len(jobQueue.Get("p")); n != 1 {
		t.Fatalf("expected 1 queued job, got %d", n)
	}
}

func TestGetJobReturnsJobWithContentType(t *testing.T) {
	newQueue(t, 4, "p")
	c, _ := newContext(t, "POST", "p", strings.NewReader("hello"), "text/plain")
	call(t, postJob, c)

	c, rec := newContext(t, "GET", "p", nil, "")
	call(t, getJob, c)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("expected body %q, got %q", "hello", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("expected Content-Type text/plain, got %q", ct)
	}
}

func TestGetJobDefaultContentType(t *testing.T) {
	newQueue(t, 4, "p")
	c, _ := newContext(t, "POST", "p", strings.NewReader("hello"), "")
	call(t, postJob, c)

	c, rec := newContext(t, "GET", "p", nil, "")
	call(t, getJob, c)
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("expected Content-Type application/octet-stream, got %q", ct)
	}
}

func TestPostJobEmptyPayload(t *testing.T) {
	newQueue(t, 4, "p")
	c, rec := newContext(t, "POST", "p", strings.NewReader(""), "text/plain")
	call(t, postJob, c)
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPostJobTooLarge(t *testing.T) {
	newQueue(t, 4, "p")
	body := bytes.Repeat([]byte("x"), int(maxBodyBytes)+1)
	c, rec := newContext(t, "POST", "p", bytes.NewReader(body), "application/octet-stream")
	call(t, postJob, c)
	if rec.Code != 413 {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestPostJobQueueFull(t *testing.T) {
	newQueue(t, 1, "p")

	c, rec := newContext(t, "POST", "p", strings.NewReader("one"), "text/plain")
	call(t, postJob, c)
	if rec.Code != 201 {
		t.Fatalf("expected 201 for first job, got %d", rec.Code)
	}

	c, rec = newContext(t, "POST", "p", strings.NewReader("two"), "text/plain")
	call(t, postJob, c)
	if rec.Code != 503 {
		t.Fatalf("expected 503 for full queue, got %d", rec.Code)
	}
}

func TestDeleteJobsDrains(t *testing.T) {
	newQueue(t, 4, "p")
	for _, body := range []string{"one", "two"} {
		c, rec := newContext(t, "POST", "p", strings.NewReader(body), "text/plain")
		call(t, postJob, c)
		if rec.Code != 201 {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
	}

	ch := jobQueue.Get("p")
	c, rec := newContext(t, "DELETE", "p", nil, "")
	call(t, deleteJobs, c)
	if rec.Code != 204 {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if len(ch) != 0 {
		t.Fatalf("expected drained queue, len=%d", len(ch))
	}
	if jobQueue.Get("p") != ch {
		t.Fatal("DELETE must keep the channel identity")
	}

	c, rec = newContext(t, "POST", "p", strings.NewReader("three"), "text/plain")
	call(t, postJob, c)
	if rec.Code != 201 {
		t.Fatalf("expected 201 after drain, got %d", rec.Code)
	}
}

func TestDeleteJobsUnknownPrinter(t *testing.T) {
	newQueue(t, 4, "p")
	c, rec := newContext(t, "DELETE", "nope", nil, "")
	call(t, deleteJobs, c)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetJobPollTimeoutReturns204(t *testing.T) {
	newQueue(t, 4, "p")
	pollTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pollTimeout = 60 * time.Second })

	c, rec := newContext(t, "GET", "p", nil, "")
	call(t, getJob, c)
	if rec.Code != 204 {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

// Regression for the orphaned-long-poll bug: Clear used to swap the channel,
// leaving a poll parked on the abandoned one.
func TestGetJobNotOrphanedByClear(t *testing.T) {
	newQueue(t, 2, "p")

	res := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetParamNames("printer")
		c.SetParamValues("p")
		if err := getJob(c); err != nil {
			c.Echo().DefaultHTTPErrorHandler(err, c)
		}
		res <- rec
	}()

	time.Sleep(100 * time.Millisecond) // let the poll park on the channel

	jobQueue.Clear("p")
	c, _ := newContext(t, "POST", "p", strings.NewReader("AFTER"), "")
	call(t, postJob, c)

	select {
	case rec := <-res:
		if rec.Code != 200 || rec.Body.String() != "AFTER" {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long-poll was orphaned by Clear")
	}
}

// Regression for the early-flush bug: flushing before the poll decides writes the
// status line to the wire while echo still considers the response uncommitted, so
// the job/idle write is dropped as a superfluous WriteHeader and the job loses the
// Content-Type set by the delivery. See AGENTS.md invariant 3.
func TestGetJobDoesNotFlushBeforeDeciding(t *testing.T) {
	newQueue(t, 4, "p")
	pollTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pollTimeout = 60 * time.Second })

	c, rec := newContext(t, "GET", "p", nil, "")
	call(t, getJob, c)
	if rec.Flushed {
		t.Fatal("idle poll flushed the response before deciding")
	}
	if rec.Code != 204 {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	c, _ = newContext(t, "POST", "p", strings.NewReader("hello"), "text/plain")
	call(t, postJob, c)

	c, rec = newContext(t, "GET", "p", nil, "")
	call(t, getJob, c)
	if rec.Flushed {
		t.Fatal("job delivery flushed the response before writing the job")
	}
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("expected 200 text/plain, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
