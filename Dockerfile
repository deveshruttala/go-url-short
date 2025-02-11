# Use an official Golang image as the base for building the applications
FROM golang:1.23 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go modules and application source code into the container
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

# Build the server and CLI binaries
RUN go build -ldflags "-s -w" -o bin/server ./cmd/server/main.go
RUN go build -ldflags "-s -w" -o bin/cli ./cmd/cli/main.go

FROM node:20-alpine

# Install Memcached and PM2
RUN apk add --no-cache memcached \
    && npm install -g pm2

# Set up the application directory
WORKDIR /app

COPY --from=builder /app/bin/server /usr/local/bin/
COPY --from=builder /app/bin/cli /usr/local/bin/
COPY --from=builder /app .

RUN chmod +x /usr/local/bin/cli
RUN cat /app/pm2.json

# Expose the required ports
EXPOSE 9091 11211

# Run the seed script and then start the server and Memcached
CMD sh /app/start.sh
