# ── Stage 1: Build ──────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev ca-certificates git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is required for SQLite (GORM driver).
# Static linking ensures the binary runs on the scratch-like alpine runtime.
RUN CGO_ENABLED=1 go build -mod=mod -trimpath \
    -ldflags="-s -w -extldflags '-static'" \
    -o /usr/local/bin/genie .

# ── Stage 2: Runtime ────────────────────────────────────────
FROM alpine:3.23

RUN apk add --no-cache ca-certificates

# Run as a non-root user for security best practices.
RUN addgroup -S genie && adduser -S -G genie -u 65532 genie \
    && mkdir -p /home/genie/.config /workspace \
    && chown -R genie:genie /home/genie /workspace

COPY --from=builder /usr/local/bin/genie /usr/local/bin/genie

USER genie

# Default config directory
VOLUME ["/home/genie/.config"]

# Default working directory for project files
WORKDIR /workspace

# AG-UI server port
EXPOSE 8080

ENTRYPOINT ["genie"]

CMD ["grant"]
