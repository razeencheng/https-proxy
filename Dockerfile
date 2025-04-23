FROM golang:1.19-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files if they exist
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o https-proxy .

# Create final lightweight image
FROM alpine:3.16

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create directories
RUN mkdir -p /app/certs /app/stats /app/templates

# Copy binary from builder stage
COPY --from=builder /app/https-proxy /app/
COPY --from=builder /app/templates/ /app/templates/
COPY config.sample.json /app/config.json

# Create non-root user
RUN addgroup -g 1000 proxy && \
    adduser -u 1000 -G proxy -s /bin/sh -D proxy && \
    chown -R proxy:proxy /app

# Set permissions
RUN chmod +x /app/https-proxy

# Switch to non-root user
USER proxy

# Expose proxy and admin ports
EXPOSE 8443 8444

# Set volume for certificates and stats
VOLUME ["/app/certs", "/app/stats"]

# Command to run
ENTRYPOINT ["/app/https-proxy"] 