package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

var jobQueue *JobQueue

func main() {
	slog.Info("http-print-server starting...")
	jobQueue = NewJobQueue(100)
	printers := os.Getenv("PRINTERS")
	if printers == "" {
		slog.Error("PRINTERS environment variable is not set. Exiting.")
		os.Exit(1)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		slog.Error("API_KEY environment variable is not set. Exiting.")
		os.Exit(1)
	}

	for _, printer := range strings.Fields(printers) {
		jobQueue.Init(printer)
		slog.Info("Queue initialized", "printer", printer)
	}

	e := echo.New()
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper: func(c *echo.Context) bool {
			return c.Path() == "/health"
		},
		LogStatus:   true,
		LogURI:      true,
		LogMethod:   true,
		LogRemoteIP: true,
		LogLatency:  true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			if v.Status < 500 {
				slog.LogAttrs(context.Background(), slog.LevelInfo, "request",
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
					slog.String("remote_ip", v.RemoteIP),
				)
			} else {
				slog.LogAttrs(context.Background(), slog.LevelError, "request",
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
					slog.String("remote_ip", v.RemoteIP),
					slog.Any("err", v.Error),
				)
			}
			return nil
		},
	}))
	e.Use(middleware.Recover(), middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	e.GET("/health", healthHandler)
	api := e.Group("/api", middleware.KeyAuth(func(c *echo.Context, key string, source middleware.ExtractorSource) (bool, error) {
		return subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) == 1, nil
	}))
	api.GET("/printers/:printer", getJob)
	api.POST("/printers/:printer", postJob)
	api.DELETE("/printers/:printer", deleteJobs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Shutting down server...")
		// Release parked long-polls with 204 before graceful shutdown drains the connections.
		close(shutdownCh)
		cancel()
	}()

	sc := echo.StartConfig{
		Address:         ":" + port,
		GracefulTimeout: 10 * time.Second,
		BeforeServeFunc: func(s *http.Server) error {
			// ReadTimeout/WriteTimeout would kill the 60 s long-poll.
			s.ReadTimeout = 0
			s.ReadHeaderTimeout = 10 * time.Second
			s.IdleTimeout = 120 * time.Second
			return nil
		},
	}
	if err := sc.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Starting server", slog.Any("err", err))
		os.Exit(1)
	}
}
