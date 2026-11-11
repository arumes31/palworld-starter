# Build stage
FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS builder

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
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

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
