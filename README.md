# http-print-server
This is an HTTP printing service that has been specially developed for label printers.

## Configuration

| Variable   | Required | Default | Description                        |
|------------|----------|---------|------------------------------------|
| `PRINTERS` | yes      | —       | Space-separated printer names      |
| `API_KEY`  | yes      | —       | API key for all `/api` endpoints    |
| `PORT`     | no       | `8080`  | Listen port                        |

## API

All `/api` endpoints require `Authorization: Bearer <API_KEY>`.

| Method   | Path                    | Description                                                        |
|----------|-------------------------|--------------------------------------------------------------------|
| `GET`    | `/health`               | Liveness probe (no auth)                                           |
| `GET`    | `/api/printers/:printer`| Long-poll: next queued job; `204 No Content` after 60 s if none   |
| `POST`   | `/api/printers/:printer`| Enqueue a job; body is the raw print payload                       |
| `DELETE` | `/api/printers/:printer`| Discard all queued jobs                                            |

Status codes: `201` enqueued, `200` job body (with the stored `Content-Type`),
`204` no pending job / queue cleared / shutting down, `400` empty or unreadable
payload, `404` unknown printer, `413` payload over 1 MiB, `503` queue full
(100 jobs), `429` rate limit exceeded (20 req/s per IP).

Jobs are held in memory only and are lost on restart. Each job is delivered to
exactly one poller.
