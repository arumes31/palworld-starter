# Build stage
FROM golang:1.26.4-alpine AS builder

WORKDIR /build

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application as a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o palworld-starter main.go

# Final stage
FROM alpine:3.22


# Install ca-certificates for external Discord API calls
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary and frontend assets from builder and host
COPY --from=builder /build/palworld-starter .
COPY templates ./templates
COPY static ./static

# Expose port 5000
EXPOSE 5000

# Run the application
CMD ["./palworld-starter"]
