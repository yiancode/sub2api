#!/bin/bash
#
# Sub2API 零停机部署脚本
# 先上传到临时位置，再原子替换，停机时间控制在数秒内。
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# ============================================================
# 默认配置
# 可通过 deploy/deploy.local.conf 覆盖
# ============================================================
REMOTE_HOST="YOUR_SERVER_IP"
REMOTE_USER="root"
REMOTE_DIR="/www/wwwroot/s2.ai80.vip"
PUBLIC_URL="https://s2.ai80.vip"
SSH_KEY=""

BINARY_NAME="sub2api"
TARGET_GOOS="linux"
TARGET_GOARCH="amd64"
LAUNCH_WRAPPER_NAME="run-sub2api.sh"

# 进程管理器：supervisor | systemd | nohup
PROCESS_MANAGER="supervisor"
SUPERVISORCTL="/www/server/panel/pyenv/bin/supervisorctl"
SUPERVISOR_PROGRAM=""
SUPERVISOR_PROFILE_DIR="/www/server/panel/plugin/supervisor/profile"
SYSTEMD_SERVICE=""

# 可选自定义启动/停止命令。设置后优先级高于 PROCESS_MANAGER。
REMOTE_STOP_COMMAND=""
REMOTE_START_COMMAND=""

GIN_MODE="release"
SERVER_HOST="127.0.0.1"
SERVER_PORT="8526"
DATA_DIR="/etc/sub2api"
HEALTHCHECK_URL=""
RUNTIME_LOG_FILE=""
STARTUP_WAIT_SECONDS="6"
LOCAL_WRAPPER_FILE=""

if [ -f "$SCRIPT_DIR/deploy.local.conf" ]; then
    # shellcheck disable=SC1091
    source "$SCRIPT_DIR/deploy.local.conf"
fi

: "${SUPERVISOR_PROGRAM:=$BINARY_NAME}"
: "${SYSTEMD_SERVICE:=$BINARY_NAME}"
: "${RUNTIME_LOG_FILE:=/tmp/${BINARY_NAME}.log}"
: "${LAUNCH_WRAPPER_NAME:=run-sub2api.sh}"
: "${SUPERVISOR_PROFILE_DIR:=/www/server/panel/plugin/supervisor/profile}"

REMOTE_BINARY_PATH="${REMOTE_DIR}/${BINARY_NAME}"
REMOTE_LAUNCH_WRAPPER_PATH="${REMOTE_DIR}/${LAUNCH_WRAPPER_NAME}"

if [ -z "${HEALTHCHECK_URL}" ] && [ -n "${SERVER_PORT}" ]; then
    HEALTHCHECK_URL="http://127.0.0.1:${SERVER_PORT}/health"
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }

cleanup_local_temp_files() {
    if [ -n "${LOCAL_WRAPPER_FILE}" ] && [ -f "${LOCAL_WRAPPER_FILE}" ]; then
        rm -f "${LOCAL_WRAPPER_FILE}"
    fi
}

trap cleanup_local_temp_files EXIT

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        print_error "未找到命令: $1"
        exit 1
    fi
}

validate_config() {
    if [ -z "${REMOTE_HOST}" ] || [ "${REMOTE_HOST}" = "YOUR_SERVER_IP" ]; then
        print_error "请先在 deploy/deploy.local.conf 中设置 REMOTE_HOST。"
        exit 1
    fi

    case "${PROCESS_MANAGER}" in
        supervisor|systemd|nohup)
            ;;
        *)
            print_error "PROCESS_MANAGER 仅支持 supervisor、systemd、nohup。当前值: ${PROCESS_MANAGER}"
            exit 1
            ;;
    esac
}

ssh_cmd() {
    local -a ssh_opts=(
        -o ConnectTimeout=10
        -o ServerAliveInterval=15
        -o ServerAliveCountMax=6
    )

    if [ -n "${SSH_KEY}" ]; then
        ssh "${ssh_opts[@]}" -i "${SSH_KEY}" "${REMOTE_USER}@${REMOTE_HOST}" "$@"
    else
        ssh "${ssh_opts[@]}" "${REMOTE_USER}@${REMOTE_HOST}" "$@"
    fi
}

run_scp() {
    local use_legacy="$1"
    shift

    local -a scp_opts=(
        -o ConnectTimeout=10
        -o ServerAliveInterval=15
        -o ServerAliveCountMax=6
    )

    if [ "${use_legacy}" = true ]; then
        scp_opts=(-O "${scp_opts[@]}")
    fi

    if [ -n "${SSH_KEY}" ]; then
        scp "${scp_opts[@]}" -i "${SSH_KEY}" "$@"
    else
        scp "${scp_opts[@]}" "$@"
    fi
}

scp_cmd() {
    local max_attempts=3
    local attempt=1
    local legacy_mode=false

    while [ "${attempt}" -le "${max_attempts}" ]; do
        if [ "${attempt}" -ge 2 ]; then
            legacy_mode=true
        fi

        if run_scp "${legacy_mode}" "$@"; then
            return 0
        fi

        if [ "${legacy_mode}" = false ]; then
            print_warning "SCP 默认模式失败，下一次将使用 -O 兼容模式重试。"
        else
            print_warning "上传失败，准备第 ${attempt}/${max_attempts} 次重试..."
        fi

        attempt=$((attempt + 1))
        sleep 2
    done

    return 1
}

ensure_local_binary_exists() {
    if [ ! -f "backend/${BINARY_NAME}" ]; then
        print_error "未找到后端二进制: backend/${BINARY_NAME}"
        print_info "请先执行完整部署，或确认 --skip-build 时该文件已存在。"
        exit 1
    fi
}

prepare_remote_dir() {
    print_info "检查远程部署目录..."
    ssh_cmd "mkdir -p '${REMOTE_DIR}' '${DATA_DIR}'"
}

remote_supervisor_program_exists() {
    ssh_cmd "${SUPERVISORCTL} status ${SUPERVISOR_PROGRAM} >/dev/null 2>&1"
}

create_local_launch_wrapper() {
    cleanup_local_temp_files
    LOCAL_WRAPPER_FILE="$(mktemp)"

    local q_gin_mode q_server_host q_server_port q_data_dir q_remote_dir q_binary_path
    printf -v q_gin_mode '%q' "${GIN_MODE}"
    printf -v q_server_host '%q' "${SERVER_HOST}"
    printf -v q_server_port '%q' "${SERVER_PORT}"
    printf -v q_data_dir '%q' "${DATA_DIR}"
    printf -v q_remote_dir '%q' "${REMOTE_DIR}"
    printf -v q_binary_path '%q' "${REMOTE_BINARY_PATH}"

    cat > "${LOCAL_WRAPPER_FILE}" <<EOF
#!/bin/bash
set -euo pipefail

export GIN_MODE=${q_gin_mode}
export SERVER_HOST=${q_server_host}
export SERVER_PORT=${q_server_port}
export DATA_DIR=${q_data_dir}

cd ${q_remote_dir}
exec ${q_binary_path} "\$@"
EOF

    chmod +x "${LOCAL_WRAPPER_FILE}"
}

upload_launch_wrapper() {
    local remote_temp_wrapper="/tmp/${LAUNCH_WRAPPER_NAME}.$$"

    create_local_launch_wrapper
    if ! scp_cmd "${LOCAL_WRAPPER_FILE}" "${REMOTE_USER}@${REMOTE_HOST}:${remote_temp_wrapper}"; then
        print_error "启动包装脚本上传失败。"
        exit 1
    fi

    ssh_cmd "chmod +x '${remote_temp_wrapper}' && mv '${remote_temp_wrapper}' '${REMOTE_LAUNCH_WRAPPER_PATH}' && chmod +x '${REMOTE_LAUNCH_WRAPPER_PATH}'"
}

remote_supervisor_config_is_safe() {
    local profile_path="${SUPERVISOR_PROFILE_DIR}/${SUPERVISOR_PROGRAM}.ini"

    ssh_cmd "[ -f '${profile_path}' ]" || return 1

    if ssh_cmd "grep -F \"command=${REMOTE_LAUNCH_WRAPPER_PATH}\" '${profile_path}' >/dev/null 2>&1"; then
        return 0
    fi

    ssh_cmd "grep -E 'SERVER_PORT[[:space:]]*=[[:space:]]*\"?${SERVER_PORT}\"?' '${profile_path}' >/dev/null 2>&1"
}

remote_kill_binary() {
    ssh_cmd "pkill -x '${BINARY_NAME}' || true"
}

remote_stop_service() {
    if [ -n "${REMOTE_STOP_COMMAND}" ]; then
        ssh_cmd "${REMOTE_STOP_COMMAND}"
        return
    fi

    case "${PROCESS_MANAGER}" in
        supervisor)
            if remote_supervisor_program_exists; then
                ssh_cmd "${SUPERVISORCTL} stop ${SUPERVISOR_PROGRAM} || true"
            else
                print_warning "Supervisor 中未找到程序 ${SUPERVISOR_PROGRAM}，改为按进程名停止。"
            fi
            remote_kill_binary
            ;;
        systemd)
            ssh_cmd "systemctl stop ${SYSTEMD_SERVICE} || true"
            remote_kill_binary
            ;;
        nohup)
            remote_kill_binary
            ;;
    esac
}

remote_start_service() {
    if [ -n "${REMOTE_START_COMMAND}" ]; then
        ssh_cmd "${REMOTE_START_COMMAND}"
        return
    fi

    case "${PROCESS_MANAGER}" in
        supervisor)
            if remote_supervisor_program_exists; then
                if ! remote_supervisor_config_is_safe; then
                    print_error "Supervisor 配置未显式绑定到 ${SERVER_PORT}。请把 command 改成 ${REMOTE_LAUNCH_WRAPPER_PATH}，或至少确认配置里写死 SERVER_PORT=${SERVER_PORT}。"
                    return 1
                fi
                ssh_cmd "${SUPERVISORCTL} start ${SUPERVISOR_PROGRAM}"
            else
                print_error "Supervisor 中未找到程序 ${SUPERVISOR_PROGRAM}。请先在宝塔 Supervisor 中创建它，或把 PROCESS_MANAGER 改成 nohup。"
                return 1
            fi
            ;;
        systemd)
            ssh_cmd "systemctl start ${SYSTEMD_SERVICE}"
            ;;
        nohup)
            ssh_cmd "cd '${REMOTE_DIR}' && nohup './${LAUNCH_WRAPPER_NAME}' >> '${RUNTIME_LOG_FILE}' 2>&1 &"
            ;;
    esac
}

remote_service_is_running() {
    ssh_cmd "pgrep -x '${BINARY_NAME}' >/dev/null"
}

remote_healthcheck() {
    if [ -z "${HEALTHCHECK_URL}" ]; then
        remote_service_is_running
        return
    fi

    if ssh_cmd "command -v curl >/dev/null 2>&1 && curl -fsS --max-time 5 '${HEALTHCHECK_URL}' >/dev/null"; then
        return
    fi

    print_warning "健康检查失败，回退到进程存活检查。"
    remote_service_is_running
}

rollback_binary() {
    print_warning "尝试回滚到备份版本..."
    ssh_cmd "[ -f '${REMOTE_BINARY_PATH}.backup' ] && mv '${REMOTE_BINARY_PATH}.backup' '${REMOTE_BINARY_PATH}'" || true
    remote_start_service || true
}

main() {
    echo ""
    echo "=============================================="
    echo "   Sub2API 零停机部署 (原子替换)"
    echo "=============================================="
    echo ""

    local skip_frontend=false
    local skip_build=false

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-frontend)
                skip_frontend=true
                shift
                ;;
            --skip-build)
                skip_build=true
                shift
                ;;
            --help|-h)
                echo "用法: $0 [选项]"
                echo ""
                echo "选项:"
                echo "  --skip-frontend  跳过前端编译"
                echo "  --skip-build     跳过所有编译，只部署"
                echo "  -h, --help       显示帮助"
                exit 0
                ;;
            *)
                print_error "未知参数: $1"
                exit 1
                ;;
        esac
    done

    cd "${PROJECT_ROOT}"

    validate_config

    print_info "项目目录: ${PROJECT_ROOT}"
    print_info "目标服务器: ${REMOTE_USER}@${REMOTE_HOST}"
    print_info "部署目录: ${REMOTE_DIR}"
    print_info "进程管理: ${PROCESS_MANAGER}"
    print_info "运行绑定: ${SERVER_HOST}:${SERVER_PORT}"
    if [ -n "${HEALTHCHECK_URL}" ]; then
        print_info "健康检查: ${HEALTHCHECK_URL}"
    fi
    echo ""

    require_cmd ssh
    require_cmd scp

    if [ "${skip_build}" = false ] && [ "${skip_frontend}" = false ]; then
        require_cmd pnpm
        print_info "Step 1/5: 编译前端..."
        if command -v nvm >/dev/null 2>&1 || [ -s "${HOME}/.nvm/nvm.sh" ]; then
            # shellcheck disable=SC1090
            source "${HOME}/.nvm/nvm.sh" 2>/dev/null || true
            nvm use 20 2>/dev/null || nvm use 18 2>/dev/null || true
        fi
        cd frontend
        pnpm install --frozen-lockfile 2>/dev/null || pnpm install
        pnpm run build
        cd "${PROJECT_ROOT}"
        print_success "前端编译完成"
    else
        print_warning "Step 1/5: 跳过前端编译"
    fi

    if [ "${skip_build}" = false ]; then
        require_cmd go
        print_info "Step 2/5: 编译后端 (${TARGET_GOOS}/${TARGET_GOARCH})..."
        cd backend
        CGO_ENABLED=0 GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" go build -buildvcs=false -tags embed -o "${BINARY_NAME}" ./cmd/server
        cd "${PROJECT_ROOT}"
        print_success "后端编译完成: backend/${BINARY_NAME}"
    else
        print_warning "Step 2/5: 跳过后端编译"
    fi

    print_info "Step 3/5: 上传到临时位置（服务保持运行）..."
    ensure_local_binary_exists
    prepare_remote_dir

    local temp_file="/tmp/${BINARY_NAME}.new.$$"
    if ! scp_cmd "backend/${BINARY_NAME}" "${REMOTE_USER}@${REMOTE_HOST}:${temp_file}"; then
        print_error "上传失败：连接中断或 SCP 协议不兼容。"
        print_info "可手动测试：scp -O backend/${BINARY_NAME} ${REMOTE_USER}@${REMOTE_HOST}:${temp_file}"
        exit 1
    fi
    ssh_cmd "chmod +x '${temp_file}'"
    upload_launch_wrapper
    print_success "上传完成: ${temp_file}"

    print_info "Step 4/5: 原子替换并重启服务..."
    ssh_cmd "[ -f '${REMOTE_BINARY_PATH}' ] && cp '${REMOTE_BINARY_PATH}' '${REMOTE_BINARY_PATH}.backup' || true"

    print_info "停止服务..."
    remote_stop_service
    sleep 1

    print_info "替换二进制..."
    ssh_cmd "mv '${temp_file}' '${REMOTE_BINARY_PATH}' && chmod +x '${REMOTE_BINARY_PATH}'"

    print_info "启动服务..."
    remote_start_service

    print_info "Step 5/5: 验证服务状态..."
    sleep "${STARTUP_WAIT_SECONDS}"

    if remote_healthcheck; then
        print_success "服务运行正常"
        echo ""
        print_success "部署完成，停机时间通常为 3-5 秒"
        if [ -n "${PUBLIC_URL}" ]; then
            echo "访问地址: ${PUBLIC_URL}"
        fi
        echo ""
        exit 0
    fi

    print_error "服务启动失败"
    ssh_cmd "tail -30 '${RUNTIME_LOG_FILE}'" || true
    rollback_binary
    exit 1
}

main "$@"
