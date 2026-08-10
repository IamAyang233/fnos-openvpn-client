#!/bin/bash
# openvpn-client-supervisor.sh —— 管理 Web 后端（nobody 降权），自愈重启
# 由框架（common start_daemon）以 root 拉起；Web 后端以 nobody 运行。
# 注意：不要 mknod TUN（seccomp 受限挂起），openvpn 客户端进程自己建 TUN。
#
# v0.1.1：补齐框架 stop 后的孤儿清理（复刻服务端 supervisor 的信号转发）：
# 框架 stop/uninstall 只杀 supervisor 本身，runuser 起的 web 是孙进程，
# 无 trap 转发时 web 变孤儿继续占 18081 → 浏览器轮询仍有响应 → "卸载后仍显示连接中"。

APP_BIN_DIR="${TRIM_APPDEST}/bin"
ETC_DIR="${TRIM_PKGVAR}/etc"
WEB_PORT="${TRIM_SERVICE_PORT:-18081}"
WEB_LOG="${ETC_DIR}/web_out.log"
# v0.1.5 统一网关模式：socket 放 target 根目录（TRIM_APPDEST），fnOS 网关经
# /app/openvpn-client 转发；TCP 仅回环（供本地调试/回调），不再对外暴露 18081。
export SOCKET_PATH="${TRIM_APPDEST}/app.sock"
export GATEWAY_PREFIX="/app/openvpn-client"

mkdir -p "${ETC_DIR}" 2>/dev/null || true
# web(nobody) 需要在 TRIM_APPDEST 根目录创建网关 socket（app.sock）
chown nobody:nogroup "${TRIM_APPDEST}" 2>/dev/null || true

# 启动前清理残留：框架 stop 只杀 supervisor 本身，runuser 起的 web 是孙进程会成孤儿占端口。
# 纯 /proc 扫描（不依赖 pkill/procps，避免 noacl 卷/seccomp 环境挂起）。
# 客户端连接进程限定 --config.*configs/ 匹配（服务端是 server.conf，勿误杀同机服务端）。
kill_stale() {
    local self=$$ pid cmd
    for p in /proc/[0-9]*; do
        pid="${p#/proc/}"
        [ "$pid" = "$self" ] && continue
        cmd=$(tr '\0' ' ' < "$p/cmdline" 2>/dev/null) || continue
        [ -z "$cmd" ] && continue
        case "$cmd" in
            *openvpn-client-supervisor.sh*) continue ;;
            *openvpn-client-web*)
                kill -9 "$pid" 2>/dev/null
                ;;
            *openvpn*--config*configs/*)
                kill -9 "$pid" 2>/dev/null
                ;;
        esac
    done
}
kill_stale

# 日志防爆：WEB_LOG 追加模式永不截断，长期运行撑爆磁盘 → 状态写失败。
# 超过 10MB 截断；截断后属主不变，运行中的进程 fd 仍可继续写入。
trim_log() {
    local f="$1" max="${2:-10485760}"
    [ -f "$f" ] || return 0
    [ "$(stat -c %s "$f" 2>/dev/null || echo 0)" -gt "$max" ] && : > "$f" 2>/dev/null || true
}

# 信号转发：框架 stop/uninstall 发 TERM → 清掉孙进程（web + 连接进程），防孤儿。
cleanup() {
    kill_stale
    wait 2>/dev/null
    exit 0
}
trap cleanup TERM INT

while :; do
    if ! pgrep -f "openvpn-client-web" >/dev/null 2>&1; then
        trim_log "${WEB_LOG}"
        chown -R nobody:nogroup "${ETC_DIR}" 2>/dev/null || true
        runuser -u nobody -- env TRIM_PKGVAR="${TRIM_PKGVAR}" TRIM_APPDEST="${TRIM_APPDEST}" \
            SOCKET_PATH="${SOCKET_PATH}" GATEWAY_PREFIX="${GATEWAY_PREFIX}" \
            OVPN_CLIENT_BIND="127.0.0.1:${WEB_PORT}" OVPN_CLIENT_TOKEN="fnos-openvpn-client" \
            "${APP_BIN_DIR}/openvpn-client-web" >> "${WEB_LOG}" 2>&1 &
    fi
    sleep 5
done
