# ── Stage 1: Build ──────────────────────────────────────────
FROM golang:1.26-alpine@sha256:d4c4845f5d60c6a974c6000ce58ae079328d03ab7f721a0734277e69905473e5 AS builder

RUN apk add --no-cache gcc musl-dev ca-certificates git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG GIT_VERSION="unknown"

# CGO is required for SQLite (GORM driver).
# Static linking ensures the binary runs on the scratch-like alpine runtime.
RUN CGO_ENABLED=1 go build -mod=mod -trimpath \
    -ldflags="-s -w -extldflags '-static' -X 'github.com/stackgenhq/genie/pkg/config.Version=${GIT_VERSION}' -X 'github.com/stackgenhq/genie/pkg/config.BuildDate=$(date +%D)'" \
    -o /usr/local/bin/genie .

# ── Stage 2: Runtime ────────────────────────────────────────
FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

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
EXPOSE 9876

ENTRYPOINT ["genie"]

CMD ["grant"]
