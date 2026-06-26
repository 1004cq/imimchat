# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 go build -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -o /api ./cmd/api
RUN CGO_ENABLED=0 go build -o /mtproto-gateway ./cmd/mtproto-gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /gateway /usr/local/bin/neomsg-gateway
COPY --from=builder /api /usr/local/bin/neomsg-api
COPY --from=builder /mtproto-gateway /usr/local/bin/neomsg-mtproto-gateway
COPY migrations/ /migrations/
