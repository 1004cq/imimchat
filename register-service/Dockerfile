# ---- 构建阶段 ----
FROM python:3.11-slim AS builder

WORKDIR /build
COPY requirements.txt .

# 安装依赖到虚拟环境
RUN python -m venv /opt/venv && \
    /opt/venv/bin/pip install --no-cache-dir -r requirements.txt

# ---- 运行阶段 ----
FROM python:3.11-slim

# 安全：创建非 root 用户
RUN groupadd -r appuser && useradd -r -g appuser -d /app appuser

WORKDIR /app

# 从构建阶段复制虚拟环境
COPY --from=builder /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# 复制应用代码
COPY app/ ./app/

# 设置权限
RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 9091

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:9091/register/health')" || exit 1

# 使用单 worker 模式（内存存储需单进程，后续换 Redis 后可增加 worker）
CMD ["gunicorn", "-w", "1", "-b", "0.0.0.0:9091", \
     "--timeout", "30", \
     "--access-logfile", "-", \
     "--error-logfile", "-", \
     "--log-level", "info", \
     "app:app"]
