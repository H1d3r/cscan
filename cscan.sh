#!/bin/bash

# CSCAN 一键管理脚本
# 功能：安装、升级、卸载、版本检查

# 版本信息
SCRIPT_VERSION="1.0"
COMPOSE_FILE="docker-compose.yaml"
SERVICES=("cscan-api" "cscan-rpc" "cscan-web")
GITHUB_REPO="tangxiaofeng7/cscan"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/main"
REGISTRY="registry.cn-hangzhou.aliyuncs.com/txf7"

# 颜色输出
info() { echo -e "\033[32m[CSCAN]\033[0m $*"; }
warning() { echo -e "\033[33m[CSCAN]\033[0m $*"; }
error() { echo -e "\033[31m[CSCAN]\033[0m $*"; }
abort() { error "$*"; exit 1; }

# 确认提示
confirm() {
    echo -e -n "\033[36m[CSCAN] $* \033[1;36m(Y/n)\033[0m "
    read -n 1 -s opt
    echo
    [[ "$opt" =~ [yY] || "$opt" == "" ]] && return 0 || return 1
}

# 检查命令是否存在
command_exists() { command -v "$1" >/dev/null 2>&1; }

# 获取本地IP
get_local_ips() {
    if command_exists ip; then
        ip addr show | grep -Eo 'inet ([0-9]*\.){3}[0-9]*' | awk '{print $2}' | grep -v '127.0.0.1'
    else
        ifconfig -a | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | awk '{print $2}' | grep -v '127.0.0.1'
    fi
}

# 检查Docker环境
check_docker() {
    if ! command_exists docker; then
        warning "未检测到 Docker"
        if confirm "是否自动安装 Docker?"; then
            install_docker
        else
            abort "请先安装 Docker"
        fi
    fi
    
    docker version >/dev/null 2>&1 || abort "Docker 服务异常，请检查 Docker 是否正常运行"
    
    # 检查 docker compose
    if docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    elif command_exists docker-compose; then
        COMPOSE_CMD="docker-compose"
    else
        warning "未检测到 Docker Compose"
        if confirm "是否自动安装 Docker Compose?"; then
            install_docker_compose
        else
            abort "请先安装 Docker Compose"
        fi
    fi
    
    info "Docker 环境检查通过"
}

# 安装Docker
install_docker() {
    info "正在安装 Docker..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker
    systemctl start docker
    docker version >/dev/null 2>&1 || abort "Docker 安装失败"
    info "Docker 安装成功"
}

# 安装Docker Compose
install_docker_compose() {
    info "正在安装 Docker Compose..."
    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
    ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose
    docker-compose version >/dev/null 2>&1 || abort "Docker Compose 安装失败"
    COMPOSE_CMD="docker-compose"
    info "Docker Compose 安装成功"
}

# 获取本地版本（从容器标签读取）
get_local_version() {
    # 尝试从cscan_api容器的镜像标签获取版本
    local image=$(docker inspect --format='{{.Config.Image}}' "cscan_api" 2>/dev/null)
    if [ -n "$image" ]; then
        # 提取标签，如 registry.cn-hangzhou.aliyuncs.com/txf7/cscan-api:V1.0 -> V1.0
        local tag=$(echo "$image" | sed 's/.*://')
        if [ "$tag" != "latest" ] && [ -n "$tag" ]; then
            echo "$tag"
            return
        fi
    fi
    
    # 如果是latest标签，尝试从镜像创建时间判断
    local created=$(docker inspect --format='{{.Created}}' "cscan_api" 2>/dev/null | cut -d'T' -f1)
    if [ -n "$created" ]; then
        echo "latest ($created)"
    else
        echo "未安装"
    fi
}

# 获取远程最新版本
get_remote_version() {
    local version=$(curl -s --connect-timeout 5 "${GITHUB_RAW}/VERSION" 2>/dev/null | tr -d '\r\n ')
    if [ -n "$version" ]; then
        echo "$version"
    else
        echo "unknown"
    fi
}

# 比较版本号（返回: 0=相同, 1=本地较旧, 2=本地较新）
compare_versions() {
    local local_ver=$1
    local remote_ver=$2
    
    # 移除V前缀进行比较
    local_ver=${local_ver#V}
    local_ver=${local_ver#v}
    remote_ver=${remote_ver#V}
    remote_ver=${remote_ver#v}
    
    # 如果包含latest或unknown，无法比较
    if [[ "$local_ver" == *"latest"* ]] || [[ "$local_ver" == "unknown" ]] || [[ "$remote_ver" == "unknown" ]]; then
        echo "unknown"
        return
    fi
    
    if [ "$local_ver" = "$remote_ver" ]; then
        echo "same"
    elif [ "$(printf '%s\n' "$local_ver" "$remote_ver" | sort -V | head -n1)" = "$local_ver" ]; then
        echo "outdated"
    else
        echo "newer"
    fi
}

# 检查更新
check_update() {
    info "正在检查更新..."
    echo ""
    
    local local_ver=$(get_local_version)
    local remote_ver=$(get_remote_version)
    
    echo "----------------------------------------"
    printf "%-15s %s\n" "本地版本:" "$local_ver"
    printf "%-15s %s\n" "最新版本:" "$remote_ver"
    echo "----------------------------------------"
    
    local result=$(compare_versions "$local_ver" "$remote_ver")
    
    case $result in
        "same")
            info "当前已是最新版本"
            ;;
        "outdated")
            warning "发现新版本 $remote_ver，建议升级"
            echo ""
            if confirm "是否立即升级?"; then
                upgrade_cscan
            fi
            ;;
        "newer")
            info "本地版本较新（可能是开发版本）"
            ;;
        *)
            warning "无法确定版本状态，建议手动检查"
            ;;
    esac
}

# 一键安装
install_cscan() {
    info "开始安装 CSCAN..."
    
    if [ ! -f "$COMPOSE_FILE" ]; then
        abort "未找到 $COMPOSE_FILE，请确保在 CSCAN 目录下执行"
    fi
    
    # 获取最新版本号
    local remote_ver=$(get_remote_version)
    if [ "$remote_ver" != "unknown" ]; then
        info "将安装版本: $remote_ver"
    fi
    
    # 拉取镜像
    info "正在拉取镜像..."
    $COMPOSE_CMD pull || abort "拉取镜像失败"
    
    # 启动服务
    info "正在启动服务..."
    $COMPOSE_CMD up -d || abort "启动服务失败"
    
    # 等待服务启动
    wait_for_healthy
    
    show_install_success
}

# 显示版本信息
show_version() {
    echo ""
    local local_ver=$(get_local_version)
    local remote_ver=$(get_remote_version)
    
    info "版本信息："
    echo "----------------------------------------"
    printf "%-15s %-20s %-15s\n" "服务" "镜像标签" "状态"
    echo "----------------------------------------"
    
    for service in "${SERVICES[@]}"; do
        local container_name=""
        case $service in
            "cscan-api") container_name="cscan_api" ;;
            "cscan-rpc") container_name="cscan_rpc" ;;
            "cscan-web") container_name="cscan_web" ;;
        esac
        
        local status=$(docker inspect --format='{{.State.Status}}' "$container_name" 2>/dev/null || echo "未运行")
        local image=$(docker inspect --format='{{.Config.Image}}' "$container_name" 2>/dev/null)
        local tag=$(echo "$image" | sed 's/.*://' || echo "-")
        
        printf "%-15s %-20s %-15s\n" "$service" "$tag" "$status"
    done
    echo "----------------------------------------"
    printf "%-15s %s\n" "本地版本:" "$local_ver"
    printf "%-15s %s\n" "最新版本:" "$remote_ver"
    echo "----------------------------------------"
    
    # 版本比较提示
    local result=$(compare_versions "$local_ver" "$remote_ver")
    if [ "$result" = "outdated" ]; then
        warning "发现新版本，建议执行升级"
    fi
}

# 等待服务健康
wait_for_healthy() {
    info "等待服务启动..."
    local timeout=120
    local elapsed=0
    local interval=5
    
    while [ $elapsed -lt $timeout ]; do
        local all_running=true
        for service in "cscan_api" "cscan_rpc" "cscan_web"; do
            local status=$(docker inspect --format='{{.State.Status}}' "$service" 2>/dev/null)
            if [ "$status" != "running" ]; then
                all_running=false
                break
            fi
        done
        
        if [ "$all_running" = true ]; then
            info "所有服务已启动"
            return 0
        fi
        
        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done
    
    echo ""
    warning "部分服务可能未完全启动，请检查日志"
}

# 显示安装成功信息
show_install_success() {
    echo ""
    echo "========================================"
    info "CSCAN 安装成功！"
    echo "========================================"
    echo ""
    warning "访问地址："
    for ip in $(get_local_ips); do
        echo "  https://$ip:3443"
    done
    echo ""
    warning "默认账号: admin / 123456"
    echo ""
    warning "注意: 执行扫描前需要先部署 Worker 节点"
    echo "========================================"
}

# 一键升级
upgrade_cscan() {
    info "开始升级 CSCAN..."
    
    # 检查是否有运行中的容器
    local running=false
    for service in "cscan_api" "cscan_rpc" "cscan_web"; do
        if docker ps --format '{{.Names}}' | grep -q "^${service}$"; then
            running=true
            break
        fi
    done
    
    if [ "$running" = false ]; then
        abort "未检测到运行中的 CSCAN 服务"
    fi
    
    # 显示版本信息
    local local_ver=$(get_local_version)
    local remote_ver=$(get_remote_version)
    
    echo ""
    echo "----------------------------------------"
    printf "%-15s %s\n" "当前版本:" "$local_ver"
    printf "%-15s %s\n" "最新版本:" "$remote_ver"
    echo "----------------------------------------"
    
    local result=$(compare_versions "$local_ver" "$remote_ver")
    if [ "$result" = "same" ]; then
        info "当前已是最新版本"
        if ! confirm "是否强制重新拉取镜像?"; then
            return
        fi
    elif [ "$result" = "newer" ]; then
        warning "本地版本较新，可能是开发版本"
        if ! confirm "是否继续?"; then
            return
        fi
    fi
    
    if ! confirm "确认升级? 升级过程中服务将短暂中断"; then
        info "取消升级"
        return
    fi
    
    # 拉取最新镜像
    info "正在拉取最新镜像..."
    $COMPOSE_CMD pull "${SERVICES[@]}" || abort "拉取镜像失败"
    
    # 重启服务
    info "正在重启服务..."
    $COMPOSE_CMD up -d "${SERVICES[@]}" || abort "重启服务失败"
    
    # 清理旧的 CSCAN 镜像（只清理 cscan 相关的悬空镜像）
    info "正在清理旧的 CSCAN 镜像..."
    docker images --filter "dangling=true" --format "{{.Repository}}:{{.Tag}} {{.ID}}" 2>/dev/null | \
        grep -E "registry.cn-hangzhou.aliyuncs.com/txf7/cscan-" | \
        awk '{print $2}' | xargs -r docker rmi 2>/dev/null || true
    
    wait_for_healthy
    
    info "升级完成！"
    show_version
}

# 一键卸载
uninstall_cscan() {
    warning "此操作将删除所有 CSCAN 容器和数据！"
    
    if ! confirm "确认卸载 CSCAN?"; then
        info "取消卸载"
        return
    fi
    
    if ! confirm "是否同时删除数据卷（数据库、配置等）?"; then
        # 仅删除容器
        info "正在停止并删除容器..."
        $COMPOSE_CMD down || warning "停止容器失败"
    else
        # 删除容器和数据卷
        info "正在停止并删除容器及数据卷..."
        $COMPOSE_CMD down -v || warning "停止容器失败"
    fi
    
    if confirm "是否删除镜像?"; then
        info "正在删除镜像..."
        for service in "${SERVICES[@]}"; do
            docker rmi "registry.cn-hangzhou.aliyuncs.com/txf7/${service}:latest" 2>/dev/null
        done
        docker rmi "docker.1ms.run/redis:7-alpine" 2>/dev/null
        docker rmi "docker.1ms.run/mongo:6" 2>/dev/null
    fi
    
    info "CSCAN 已卸载"
}

# 查看日志
show_logs() {
    echo "选择要查看的服务日志："
    echo "1. cscan-api"
    echo "2. cscan-rpc"
    echo "3. cscan-web"
    echo "4. 所有服务"
    echo "0. 返回"
    echo -n "请选择: "
    read opt
    
    case $opt in
        1) docker logs -f --tail 100 cscan_api ;;
        2) docker logs -f --tail 100 cscan_rpc ;;
        3) docker logs -f --tail 100 cscan_web ;;
        4) $COMPOSE_CMD logs -f --tail 100 ;;
        0) return ;;
        *) warning "无效选项" ;;
    esac
}

# 重启服务
restart_cscan() {
    info "正在重启服务..."
    $COMPOSE_CMD restart "${SERVICES[@]}" || abort "重启失败"
    info "重启完成"
}

# 停止服务
stop_cscan() {
    info "正在停止服务..."
    $COMPOSE_CMD stop || abort "停止失败"
    info "服务已停止"
}

# 启动服务
start_cscan() {
    info "正在启动服务..."
    $COMPOSE_CMD up -d || abort "启动失败"
    wait_for_healthy
    info "服务已启动"
}

# 显示状态
show_status() {
    echo ""
    $COMPOSE_CMD ps
    show_version
}

# 主菜单
main_menu() {
    clear
    
    # 获取版本信息
    local local_ver=$(get_local_version 2>/dev/null)
    local remote_ver=$(get_remote_version 2>/dev/null)
    local update_hint=""
    
    local result=$(compare_versions "$local_ver" "$remote_ver" 2>/dev/null)
    if [ "$result" = "outdated" ]; then
        update_hint=" \033[33m[有新版本: $remote_ver]\033[0m"
    fi
    
    echo ""
    echo "  ██████╗███████╗ ██████╗ █████╗ ███╗   ██╗"
    echo " ██╔════╝██╔════╝██╔════╝██╔══██╗████╗  ██║"
    echo " ██║     ███████╗██║     ███████║██╔██╗ ██║"
    echo " ██║     ╚════██║██║     ██╔══██║██║╚██╗██║"
    echo " ╚██████╗███████║╚██████╗██║  ██║██║ ╚████║"
    echo "  ╚═════╝╚══════╝ ╚═════╝╚═╝  ╚═╝╚═╝  ╚═══╝"
    echo ""
    echo -e "\033[33m===================================================="
    echo " CSCAN 管理工具 v${SCRIPT_VERSION}"
    echo " 企业级分布式网络资产扫描平台"
    echo -e " 当前版本: ${local_ver}${update_hint}"
    echo " 当前时间: $(date +"%Y-%m-%d %H:%M:%S")"
    echo "====================================================\033[0m"
    echo ""
    echo " 1. 一键安装"
    echo " 2. 一键升级"
    echo " 3. 一键卸载"
    echo " 4. 查看状态"
    echo " 5. 查看日志"
    echo " 6. 启动服务"
    echo " 7. 停止服务"
    echo " 8. 重启服务"
    echo " 9. 检查更新"
    echo " 0. 退出"
    echo ""
    echo -n " 请输入选项: "
    read opt
    
    case $opt in
        1) install_cscan ;;
        2) upgrade_cscan ;;
        3) uninstall_cscan ;;
        4) show_status ;;
        5) show_logs ;;
        6) start_cscan ;;
        7) stop_cscan ;;
        8) restart_cscan ;;
        9) check_update ;;
        0) exit 0 ;;
        *) warning "无效选项" ;;
    esac
    
    echo ""
    echo -e "\033[33m按任意键返回主菜单...\033[0m"
    read -n 1 -s
    main_menu
}

# 命令行参数支持
case "${1:-}" in
    install) check_docker; install_cscan ;;
    upgrade) check_docker; upgrade_cscan ;;
    uninstall) check_docker; uninstall_cscan ;;
    status) check_docker; show_status ;;
    start) check_docker; start_cscan ;;
    stop) check_docker; stop_cscan ;;
    restart) check_docker; restart_cscan ;;
    logs) check_docker; show_logs ;;
    version) show_version ;;
    help|--help|-h)
        echo "CSCAN 管理脚本"
        echo ""
        echo "用法: $0 [命令]"
        echo ""
        echo "命令:"
        echo "  install    一键安装"
        echo "  upgrade    一键升级"
        echo "  uninstall  一键卸载"
        echo "  status     查看状态"
        echo "  start      启动服务"
        echo "  stop       停止服务"
        echo "  restart    重启服务"
        echo "  logs       查看日志"
        echo "  version    查看版本"
        echo "  help       显示帮助"
        echo ""
        echo "不带参数运行将进入交互式菜单"
        ;;
    *)
        check_docker
        main_menu
        ;;
esac
