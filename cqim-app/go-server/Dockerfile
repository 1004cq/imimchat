# CQIM Go 服务端（Node 版完整替代）
FROM golang:1.23-bookworm AS builder
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 纯 Go（pgx/redis 无 CGO 依赖），禁用 CGO 以获静态二进制
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o cqim-server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata wget && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -r appuser && useradd -r -g appuser appuser
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder --chown=appuser:appuser /app/cqim-server .
# 静态文件（Web 前端构建产物）挂载到 /app/static，或用 STATIC_DIR 指定
RUN mkdir -p /app/static && chown appuser:appuser /app/static
USER appuser
EXPOSE 3000
ENV PORT=3000
CMD ["./cqim-server"]
