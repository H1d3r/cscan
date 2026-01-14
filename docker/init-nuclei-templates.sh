#!/bin/sh
# Nuclei 模板库初始化脚本
# 在容器启动时自动检测并下载 Nuclei 默认模板库

set -e

TEMPLATES_DIR="/app/data/nuclei-templates"
LOCK_FILE="/app/data/.nuclei-templates-initialized"
REGION="${NUCLEI_REGION:-auto}"  # 支持环境变量配置：auto, github, gitee

echo "[Nuclei Templates] Checking template library..."

# 如果已经初始化过且模板目录存在，跳过
if [ -f "$LOCK_FILE" ] && [ -d "$TEMPLATES_DIR" ] && [ "$(ls -A $TEMPLATES_DIR 2>/dev/null)" ]; then
    echo "[Nuclei Templates] Template library already initialized, skipping..."
    exit 0
fi

# 创建模板目录
mkdir -p "$TEMPLATES_DIR"

# 自动检测地区（通过测试 GitHub 连接速度）
detect_region() {
    echo "[Nuclei Templates] Auto-detecting region..."
    
    # 测试 GitHub 连接（超时 3 秒）
    if timeout 3 wget -q --spider https://github.com 2>/dev/null; then
        echo "[Nuclei Templates] GitHub accessible, using GitHub source"
        echo "github"
    else
        echo "[Nuclei Templates] GitHub not accessible, using Gitee mirror"
        echo "gitee"
    fi
}

# 根据地区选择下载源
if [ "$REGION" = "auto" ]; then
    REGION=$(detect_region)
fi

# 设置下载源
if [ "$REGION" = "gitee" ]; then
    REPO_URL="https://gitee.com/mirrors/nuclei-templates.git"
    echo "[Nuclei Templates] Using Gitee mirror (China)"
else
    REPO_URL="https://github.com/projectdiscovery/nuclei-templates.git"
    echo "[Nuclei Templates] Using GitHub official repository"
fi

# 下载模板库
echo "[Nuclei Templates] Downloading templates from $REPO_URL..."
if git clone --depth 1 "$REPO_URL" "$TEMPLATES_DIR" 2>&1; then
    echo "[Nuclei Templates] Templates downloaded successfully"
    
    # 统计模板数量
    TEMPLATE_COUNT=$(find "$TEMPLATES_DIR" -name "*.yaml" -o -name "*.yml" | wc -l)
    echo "[Nuclei Templates] Found $TEMPLATE_COUNT template files"
    
    # 创建锁文件，记录初始化信息
    cat > "$LOCK_FILE" <<EOF
initialized_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
source=$REPO_URL
region=$REGION
template_count=$TEMPLATE_COUNT
EOF
    
    echo "[Nuclei Templates] Initialization completed"
else
    echo "[Nuclei Templates] Failed to download templates"
    exit 1
fi
