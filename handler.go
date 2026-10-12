package main

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

var (
	maxBodyBytes int64 = 1 << 20 // 1 MiB POST body limit
	pollTimeout        = 60 * time.Second
	shutdownCh         = make(chan struct{})
)

func healthHandler(c *echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store, max-age=0")
	return c.String(http.StatusOK, "OK")
}

func postJob(c *echo.Context) error {
	jobs := jobQueue.Get(c.Param("printer"))
	if jobs == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Printer not found")
	}

	r := http.MaxBytesReader(c.Response(), c.Request().Body, maxBodyBytes)
	b, err := io.ReadAll(r)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "payload too large")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read payload")
	}
	if len(b) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "empty payload")
	}

	contentType := c.Request().Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	job := Job{ContentType: contentType, Payload: b}
	select {
	case jobs <- job:
	default:
		return echo.NewHTTPError(http.StatusServiceUnavailable, "job queue full")
	}
	return c.NoContent(http.StatusCreated)
}

func getJob(c *echo.Context) error {
	jobs := jobQueue.Get(c.Param("printer"))
	if jobs == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Printer not found")
	}

	c.Response().Header().Set("Cache-Control", "no-cache")
	t := time.NewTimer(pollTimeout)
	defer t.Stop()

	select {
	case <-c.Request().Context().Done():
		return nil
	case <-shutdownCh:
		return c.NoContent(http.StatusNoContent)
	case <-t.C:
		return c.NoContent(http.StatusNoContent)
	case j := <-jobs:
		return c.Blob(http.StatusOK, j.ContentType, j.Payload)
	}
}

func deleteJobs(c *echo.Context) error {
	if jobQueue.Get(c.Param("printer")) == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Printer not found")
	}
	jobQueue.Clear(c.Param("printer"))
	return c.NoContent(http.StatusNoContent)
}
