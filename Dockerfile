# --- Stage 1: Builder ---
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .
