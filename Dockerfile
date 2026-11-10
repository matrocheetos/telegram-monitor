# Build Stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/telegram-monitor \
    ./cmd/telegram-monitor

# Final Minimal Stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata su-exec \
    && addgroup -g 1000 -S appuser \
    && adduser -u 1000 -S -G appuser -s /bin/sh appuser \
    && mkdir -p /app/data \
    && chown -R 1000:1000 /app

COPY --from=builder /bin/telegram-monitor /bin/telegram-monitor
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /app

EXPOSE 8080

ENV DB_PATH=/app/data/telegram-monitor.db \
    HTTP_PORT=8080 \
    GOMEMLIMIT=128MiB

ENTRYPOINT ["/entrypoint.sh"]
