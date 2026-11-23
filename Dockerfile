# Build stage
FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

WORKDIR /build

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go ./
COPY internal ./internal

# Build the application as a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o palworld-starter .

# Final stage
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# Install ca-certificates for external Discord API calls, and create an
# unprivileged user (uid/gid 10001). The bundled compose files run the app as
# this user behind a Docker socket proxy. The image itself still defaults to
# root so existing deployments that mount /var/run/docker.sock directly keep
# working; see "Hardened deployment" in the README.
RUN apk --no-cache add ca-certificates &&     addgroup -S -g 10001 app &&     adduser -S -D -H -u 10001 -G app app

WORKDIR /app

# Copy the binary and frontend assets from builder and host
COPY --from=builder /build/palworld-starter .
COPY templates ./templates
COPY static ./static

# Expose port 5000
EXPOSE 5000

# Run the application
CMD ["./palworld-starter"]
