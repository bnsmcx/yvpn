# Build Stage: Use Go image to build the binary
FROM golang:1.21-alpine AS build

# Set environment variables for Go
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum to leverage caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project
COPY . .

# Build the Go binary for /cmd/tui
WORKDIR /app/cmd/tui
RUN go build -o main .

# Final Stage: Use a minimal Alpine image
FROM alpine:latest

# Set working directory
WORKDIR /app

# Copy the binary from the build stage
COPY --from=build /app/cmd/tui/main .

# Expose port 22
EXPOSE 1337

# Run the binary
CMD ["./main", "ssh"]

