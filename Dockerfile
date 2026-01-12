# --- Stage 1: Builder ---
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary. 
# -o main: output file name
# ./cmd/live/live_ethusdt_15m.go: path to your specific main file
RUN go build -o live_ethusdt_15m ./cmd/live/live_ethusdt_15m.go

# --- Stage 2: Runner (Production Image) ---
FROM alpine:latest

# Install CA certificates (Required for making HTTPS requests to Binance/AWS)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy ONLY the binary from the builder stage
COPY --from=builder /app/live_ethusdt_15m .

# Copy .env if you still use it (though AWS Secrets Manager is better)
# COPY .env . 

# Run the binary
CMD ["./live_ethusdt_15m"]