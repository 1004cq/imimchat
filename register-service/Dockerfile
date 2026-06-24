FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app.py .

EXPOSE 9091

# 使用单 worker + 多线程模式，确保内存验证码仍在同一进程内，同时避免短信/上游请求阻塞所有访问
# 生产环境建议改用 Redis 后可恢复多 worker
CMD ["gunicorn", "-w", "1", "--threads", "4", "-b", "0.0.0.0:9091", "--timeout", "30", "--access-logfile", "-", "app:app"]
