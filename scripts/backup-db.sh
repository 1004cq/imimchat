#!/bin/bash
# imimchat 数据库备份脚本
# 用途：备份 MySQL 数据库到本地文件
# 使用方法：bash scripts/backup-db.sh
# 建议：配置 cron 定时执行，例如每天凌晨 3 点：
#   0 3 * * * /home/ubuntu/tsdd/scripts/backup-db.sh >> /var/log/imimchat-backup.log 2>&1

set -e

CONTAINER="tsdd-mysql-1"
BACKUP_DIR="/home/ubuntu/backups/mysql"
DB_NAME="im"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-TsddMysql@2026}"
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/im_backup_$DATE.sql.gz"

echo "=== imimchat 数据库备份 ==="
echo "时间: $(date)"

# 创建备份目录
mkdir -p "$BACKUP_DIR"

# 执行备份
echo "备份数据库 $DB_NAME..."
docker exec "$CONTAINER" mysqldump \
  -u root -p"$MYSQL_ROOT_PASSWORD" \
  --single-transaction \
  --routines \
  --triggers \
  "$DB_NAME" | gzip > "$BACKUP_FILE"

echo "备份完成: $BACKUP_FILE ($(du -sh "$BACKUP_FILE" | cut -f1))"

# 清理 30 天前的备份
echo "清理旧备份（保留最近 30 天）..."
find "$BACKUP_DIR" -name "im_backup_*.sql.gz" -mtime +30 -delete

echo "=== 备份任务完成 ==="
