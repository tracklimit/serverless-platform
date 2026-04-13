FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/api /app/api
COPY --from=builder /out/worker /app/worker
# Default entrypoint is the API; the worker Deployment overrides with
# `command: ["/app/worker"]`.
ENTRYPOINT ["/app/api"]
