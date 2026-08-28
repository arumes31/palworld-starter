# syntax=docker/dockerfile:1.20

FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

WORKDIR /build
RUN apk add --no-cache gcc=15.2.0-r5 musl-dev=1.2.6-r2
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY cmd ./cmd
COPY internal ./internal
COPY templates ./templates
COPY static ./static
RUN go test -race ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o palworld-starter . && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o docker-broker ./cmd/docker-broker

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime

RUN apk add --no-cache \
        ca-certificates=20260611-r0 \
        libcrypto3=3.5.8-r0 \
        libssl3=3.5.8-r0 && \
    addgroup -S -g 10001 app && \
    adduser -S -D -H -u 10001 -G app app && \
    mkdir -p /data && \
    chown app:app /data
WORKDIR /app
USER 10001:10001

FROM runtime AS broker
COPY --from=builder --chown=10001:10001 /build/docker-broker ./docker-broker
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["wget", "--quiet", "--spider", "http://127.0.0.1:8081/healthz"]
CMD ["./docker-broker"]

FROM runtime AS app
COPY --from=builder --chown=10001:10001 /build/palworld-starter ./palworld-starter
# Keep the broker binary in the published image so GHCR deployments can run
# the same immutable digest with a different command for the private broker.
COPY --from=builder --chown=10001:10001 /build/docker-broker ./docker-broker
COPY --chown=10001:10001 templates ./templates
COPY --chown=10001:10001 static ./static
ENV STATE_DIR=/data
EXPOSE 5000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["wget", "--quiet", "--spider", "http://127.0.0.1:5000/healthz"]
CMD ["./palworld-starter"]
