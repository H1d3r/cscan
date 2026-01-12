#!/bin/sh

# JWT密钥持久化文件路径（挂载到volume）
JWT_SECRET_FILE="/app/data/.jwt_secret"

# 生成或读取JWT密钥
if [ -n "$JWT_SECRET" ]; then
    # 用户通过环境变量指定了密钥
    echo "[INFO] Using JWT_SECRET from environment variable"
elif [ -f "$JWT_SECRET_FILE" ]; then
    # 从持久化文件读取
    JWT_SECRET=$(cat "$JWT_SECRET_FILE")
    echo "[INFO] Loaded JWT_SECRET from persistent storage"
else
    # 首次启动，生成随机密钥并持久化
    mkdir -p /app/data
    JWT_SECRET=$(head -c 48 /dev/urandom | base64 | tr -d '\n/+=' | head -c 64)
    echo -n "$JWT_SECRET" > "$JWT_SECRET_FILE"
    chmod 600 "$JWT_SECRET_FILE"
    echo "[INFO] Generated new JWT_SECRET and saved to persistent storage"
fi

export JWT_SECRET

# 使用envsubst替换配置文件中的环境变量
envsubst '${JWT_SECRET}' < /app/etc/cscan-api.yaml.template > /app/etc/cscan-api.yaml

# 执行传入的命令
exec "$@"
