FROM golang:1.22-alpine AS builder
ENV GOPROXY=https://goproxy.cn,direct
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=1 GOOS=linux go build -o cqim-gateway ./cmd/main.go

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata sqlite-libs
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /app/cqim-gateway .
EXPOSE 8081
CMD ["./cqim-gateway"]
