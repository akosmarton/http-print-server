FROM golang:1.26 as builder

WORKDIR /app
COPY . .
RUN --mount=type=cache,target=$HOME/go/pkg/mod CGO_ENABLED=0 go build -v

FROM alpine

COPY --from=builder /app/http-print-server /app/http-print-server

ENV PRINTERS= API_KEY= PORT=8080

EXPOSE 8080

ENTRYPOINT [ "/app/http-print-server" ]
