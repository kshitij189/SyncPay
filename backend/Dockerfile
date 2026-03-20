# --- Build Stage ---
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN go build -o main .

# --- Final Stage ---
FROM alpine:latest

WORKDIR /app

# Copy the binary from the builder
COPY --from=builder /app/main .

# Copy .env if needed (though Render uses Environment Variables)
# COPY .env .

# Export the port
EXPOSE 8000

# Run the application
CMD ["./main"]
