# AGENTS.md

Guidance for coding agents working in this repository.

## Project

Single-package Go service (`package main` at the repo root). It keeps one in-memory
job queue per printer and hands raw print payloads to printer agents over a
long-poll HTTP API. Stack: Go 1.26, echo v5 (`github.com/labstack/echo/v5`).
Runtime configuration and the HTTP surface are documented in `README.md` — do not
duplicate them here.

## Commands

| Task | Command |
|------|---------|
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Test | `go test -race ./...` (always with the race detector) |
| Format check | `gofmt -l .` (must print nothing) |
| Run locally | `PRINTERS=test API_KEY=secret PORT=8080 go run .` |
| Container | `docker build -t http-print-server .` / `docker compose up` |

The `http-print-server` binary at the repo root is gitignored build output; never commit it.

## Architecture

- `main.go` — env parsing, echo wiring (request logger, `Recover`, `RateLimiter`,
  `KeyAuth` on `/api`), signal handling, graceful shutdown via `echo.StartConfig`
  (`BeforeServeFunc` owns the server timeouts, `GracefulTimeout` bounds the drain). Owns the
  `jobQueue` package global and closes `shutdownCh` on SIGINT/SIGTERM.
- `handler.go` — `healthHandler`, `postJob`, `getJob`, `deleteJobs`, plus the tunables
  `maxBodyBytes` (1 MiB), `pollTimeout` (60 s) and `shutdownCh`.
- `jobqueue.go` — `JobQueue`: per-printer buffered channels (capacity 100) guarded by a
  `sync.RWMutex`.

Invariants — violating these reintroduces already-fixed bugs:

1. **Printer channels are never replaced after startup.** `Init` runs only before
   `e.Start`; `Clear` drains the existing channel in place. Long-polls park on the
   channel returned by `Get`, so replacing it orphans in-flight pollers and loses jobs.
2. **All `JobQueue` map access goes through `Get`/`Init`/`Clear`** — never touch
   `printers` directly. The race detector is the safety net, not the design.
3. **Never flush the long-poll response before the handler decides.** An early
   `Flush()` commits `200` and drops headers set later (`Content-Type`); status must be
   written by the first body write (`c.Blob` / `c.NoContent`).
4. **`Content-Type` is always non-empty** — `postJob` defaults to
   `application/octet-stream` when the client sends none.
5. **No `WriteTimeout`/`ReadTimeout` on the server** — they would kill the 60 s
   long-poll. Only `ReadHeaderTimeout` and `IdleTimeout` are set, in
   `StartConfig.BeforeServeFunc`. echo v5's `StartConfig` defaults `ReadTimeout` to 30 s
   on its own, so `BeforeServeFunc` resets it to `0`.

## Conventions

- Keep handlers thin: validate, enqueue, return. Return failures as
  `echo.NewHTTPError(status, message)` so error bodies share one shape.
- Status codes: `201` enqueued, `200` job body, `204` idle/cleared/shutting down,
  `400` bad payload, `404` unknown printer, `413` payload too large, `429` rate limited,
  `503` queue full.
- Tests live in `package main` beside the code and drive handlers directly through
  `echo.New().NewContext()` — no HTTP server, middleware intentionally bypassed. Path
  params come from `c.SetPathValues(echo.PathValues{...})` (echo v5 dropped
  `SetParamNames`/`SetParamValues`); errors go through `c.Echo().HTTPErrorHandler(c, err)`.
- Tests must not call `t.Parallel()` and must not reassign the `jobQueue` global after
  starting a goroutine: the global is per-test state.
- Use Go 1.22+ `for range n` instead of `for i := 0; i < n; i++`.
- Code, comments, docs and commit messages in English.
- Changing the long-poll contract (e.g. the `204` idle response) affects the printer
  agent in a separate repository — confirm compatibility before changing the wire
  behavior.
