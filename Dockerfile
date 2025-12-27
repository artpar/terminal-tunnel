# Dockerfile for terminal-tunnel
# Used by goreleaser - expects pre-built binary

FROM alpine:3.20

# Install ca-certificates for HTTPS and tzdata for timezone support
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 tt

# Copy pre-built binary from goreleaser
COPY tt /usr/local/bin/tt

# Use non-root user
USER tt

# Set working directory
WORKDIR /home/tt

# Expose default relay server port
EXPOSE 8080

# Default: run relay server
ENTRYPOINT ["/usr/local/bin/tt"]
CMD ["relay", "--port", "8080"]
