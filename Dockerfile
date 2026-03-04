FROM golang:1.24-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o devnook-server .

FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=builder /app/devnook-server /usr/local/bin/devnook-server
EXPOSE 8080
CMD ["devnook-server"]
