# Build stage
FROM golang:1.26.10-alpine3.24@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS builder

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
FROM alpine:3.24@sha256:71e9b7bae160ba167fa17414401b94cbcdaf3c32feeab9298bd794a2a0614c35

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
