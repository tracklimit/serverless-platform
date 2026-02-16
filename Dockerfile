FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /serverless-platform ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /serverless-platform /serverless-platform
ENTRYPOINT ["/serverless-platform"]
