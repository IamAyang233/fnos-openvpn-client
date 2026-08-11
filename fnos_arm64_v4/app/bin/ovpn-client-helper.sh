#!/bin/bash
# ovpn-client-helper.sh —— OpenVPN 客户端特权操作（经 sudoers 白名单以 root 执行）
# 功能：connect <name> 启动 openvpn 客户端；disconnect 停止。
# 数据目录：${TRIM_PKGVAR}/etc（web 后端写入的路径，helper 以 root 也读同一份）
# 注意：sudo 会清空环境变量（env_reset），路径全部自推断，不依赖 TRIM_* 环境变量。

# 自推断数据目录（与 ovpn-helper.sh 同思路）：
# 优先级：显式环境变量 > /var/apps/openvpn-client/var symlink（官方）> pwd -P 推导 > 兜底 vol2
APP_BIN_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_LIB_DIR="$(cd "${APP_BIN_DIR}/../lib" && pwd)"
OVPN_BIN="${APP_BIN_DIR}/openvpn"
# openvpn 动态链接依赖 fpk 内置 .so（libpkcs11-helper 等）
export LD_LIBRARY_PATH="${APP_LIB_DIR}${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

OVPN_DATA=""
if [ -n "${TRIM_PKGVAR:-}" ]; then
    OVPN_DATA="${TRIM_PKGVAR}/etc"
elif [ -d "/var/apps/openvpn-client/var" ]; then
    OVPN_DATA="/var/apps/openvpn-client/var/etc"
else
    APP_REAL="$(cd "${APP_BIN_DIR}/.." && pwd -P 2>/dev/null)"
    VOL_NUM="$(echo "${APP_REAL}" | sed -n 's|^/vol\([0-9][0-9]*\)/.*|\1|p')"
    if [ -n "${VOL_NUM}" ] && [ -d "/vol${VOL_NUM}/@appdata/openvpn-client" ]; then
        OVPN_DATA="/vol${VOL_NUM}/@appdata/openvpn-client/etc"
    else
        OVPN_DATA="/vol2/@appdata/openvpn-client/etc"
    fi
fi

CONF_DIR="${OVPN_DATA}/configs"
STATUS_FILE="${OVPN_DATA}/status.json"
LOG_FILE="${OVPN_DATA}/client.log"

mkdir -p "${CONF_DIR}"

# 写连接状态
write_status() {
    local name="$1" pid="$2" connected="$3"
    cat > "${STATUS_FILE}" << EOF
{"name":"${name}","pid":${pid},"connected":${connected},"remote":"","local_ip":"","remote_ip":"","auto_connect":false,"started_at":"$(date +'%Y-%m-%d %H:%M:%S')","updated_at":"$(date +'%Y-%m-%d %H:%M:%S')"}
EOF
    chown nobody:nogroup "${STATUS_FILE}" 2>/dev/null || true
}

# 提取隧道信息（连接成功后写 status）
# v0.1.3：修复字段解析——旧逻辑抄服务端思路完全不适用客户端：
#  - remote 从 status 文件 grep ^REMOTE（客户端 status 无此行）→ 永远空 → UI"服务器地址 -"
#  - local_ip 匹配 "10.8.0.2," 行（客户端 STATISTICS 格式无此行）→ 永远空
#  - remote_ip 写死 ip addr show tun0（本机服务端也占 tun0）→ 张冠李戴
# 正确来源：服务器地址 = 配置文件的 remote 行；隧道本端/对端 IP = client.log 的
# "/sbin/ip addr add dev tunX local 10.8.0.2 peer 10.8.0.1"（真实连接信息）。
# v0.1.6：支持 IPv6 地址显示（双栈模式）。
update_tunnel_info() {
    local name="$1"
    local local_ip="" remote_ip="" remote="" pid
    local local_ip6="" remote_ip6=""
    remote=$(grep -m1 '^remote' "${CONF_DIR}/${name}.ovpn" 2>/dev/null | awk '{print $2" "$3}')
    if [ -f "${LOG_FILE}" ]; then
        # OpenVPN 2.6 客户端实际日志格式（实测）：
        #   net_addr_v4_add: 10.8.0.2/24 dev tun1        ← 隧道本端 IPv4
        #   net_addr_v6_add: fd00::2/64 dev tun1         ← 隧道本端 IPv6（双栈模式）
        #   PUSH_REPLY,...,route-gateway 10.8.0.1,...     ← 对端隧道网关 IPv4
        #   PUSH_REPLY,...,route-ipv6 ...                 ← 对端 IPv6 路由（不含网关 IP）
        local_ip=$(grep -oP 'net_addr_v4_add: \K[0-9.]+' "${LOG_FILE}" | tail -1)
        remote_ip=$(grep -oP 'route-gateway \K[0-9.]+' "${LOG_FILE}" | tail -1)
        # IPv6：优先取 net_addr_v6_add 的本地地址；对端取 ifconfig-ipv6-push 的网关（如有）
        local_ip6=$(grep -oP 'net_addr_v6_add: \K[0-9a-f:]+/\d+' "${LOG_FILE}" | tail -1 | cut -d/ -f1)
        # 客户端 PUSH_REPLY 格式为 ifconfig-ipv6 <本端>/64 <网关>；第二个即隧道对端 IPv6 网关
        remote_ip6=$(grep -oP 'ifconfig-ipv6 \K[0-9a-f:]+/\d+ [0-9a-f:]+' "${LOG_FILE}" | tail -1 | awk '{print $2}' | cut -d/ -f1)
        # 兜底：从 tun 设备直接读取
        if [ -z "${local_ip6}" ] || [ -z "${remote_ip6}" ]; then
            local tun_dev=$(grep -oP 'dev \Ktun\d+' "${LOG_FILE}" | tail -1)
            if [ -n "${tun_dev}" ]; then
                [ -z "${local_ip6}" ] && local_ip6=$(ip -6 addr show dev "${tun_dev}" 2>/dev/null | grep -oP 'inet6 \K[0-9a-f:]+/\d+' | head -1 | cut -d/ -f1)
            fi
        fi
    fi
    # 组合显示：IPv4 为主，IPv6 为辅（如有）
    local local_display="${local_ip}"
    [ -n "${local_ip6}" ] && local_display="${local_ip} / ${local_ip6}"
    local remote_display="${remote_ip}"
    [ -n "${remote_ip6}" ] && remote_display="${remote_ip} / ${remote_ip6}"
    pid=$(pgrep -f "openvpn --config.*configs/${name}.ovpn" | head -1 || echo 0)
    cat > "${STATUS_FILE}" << EOF
{"name":"${name}","pid":${pid},"connected":true,"remote":"${remote}","local_ip":"${local_display}","remote_ip":"${remote_display}","auto_connect":false,"started_at":"$(date +'%Y-%m-%d %H:%M:%S')","updated_at":"$(date +'%Y-%m-%d %H:%M:%S')"}
EOF
    chown nobody:nogroup "${STATUS_FILE}" 2>/dev/null || true
}

case "$1" in
connect)
    [ -z "$2" ] && { echo "缺少配置名"; exit 1; }
    name="$2"
    conf="${CONF_DIR}/${name}.ovpn"
    [ -f "${conf}" ] || { echo "配置不存在: ${conf}"; exit 1; }
    # 已有连接先断
    if [ -f "${STATUS_FILE}" ]; then
        old_pid=$(python3 -c "import json;print(json.load(open('${STATUS_FILE}')).get('pid',0))" 2>/dev/null)
        [ -n "${old_pid}" ] && [ "${old_pid}" -gt 0 ] && kill "${old_pid}" 2>/dev/null
    fi
    # 启动客户端（--daemon 后台 + 写日志 + status 文件；--log 覆盖保证判据 grep 本次启动日志）
    # TUN 设备由 openvpn 自动创建（mknod 受限，不能手动建）
    "${OVPN_BIN}" --config "${conf}" \
        --daemon \
        --log "${LOG_FILE}" \
        --status "${OVPN_DATA}/openvpn-status.log" 5 \
        --writepid "${OVPN_DATA}/openvpn-client.pid"
    sleep 2
    pid=$(cat "${OVPN_DATA}/openvpn-client.pid" 2>/dev/null || echo 0)
    # 真正的连接判据：client.log 出现 "Initialization Sequence Completed"（TLS 握手 + 隧道建立完成）。
    # 仅进程存活/tun0 本地配置 ≠ 已连接（纯 IPv6 不可达时进程也在跑但永远连不上）。
    # v0.1.2：快速失败——日志出现明确错误（TCP 拒绝/无路由/TLS/认证失败/DNS 解析失败）立即判定，
    # 避免连不上的服务器拖满 20 秒判据窗口（UI 一直"连接中"）。
    FAIL_PAT='TCP: connect.*failed|Connection refused|No route to host|Network is unreachable|TLS Error|AUTH_FAILED|Resolv.*failed|Could not determine IPv4/IPv6 protocol|Name or service not known'
    connected=false
    fail_reason=""
    i=0
    while [ "${i}" -lt 20 ]; do
        if grep -q 'Initialization Sequence Completed' "${LOG_FILE}" 2>/dev/null; then
            connected=true
            break
        fi
        fail_reason=$(grep -E "${FAIL_PAT}" "${LOG_FILE}" 2>/dev/null | tail -1)
        if [ -n "${fail_reason}" ]; then
            break
        fi
        if [ -n "${pid}" ] && [ "${pid}" -gt 0 ] && ! kill -0 "${pid}" 2>/dev/null; then
            fail_reason="进程提前退出（PID ${pid}）"
            break
        fi
        i=$((i + 1))
        sleep 1
    done
    if [ "${connected}" = "true" ]; then
        update_tunnel_info "${name}"
        echo "连接成功: ${name} (PID ${pid})"
        exit 0
    fi
    # 连不上：清理可能还在后台重试的连接进程（防僵尸占 pid/端口），再写断开状态。
    if [ -n "${pid}" ] && [ "${pid}" -gt 0 ] && kill -0 "${pid}" 2>/dev/null; then
        kill "${pid}" 2>/dev/null
        sleep 1
        kill -9 "${pid}" 2>/dev/null || true
    fi
    write_status "${name}" "0" "false"
    # 不加"连接失败:"前缀——apiConnect 已包装，避免 toast 文案重复
    echo "${fail_reason:-连接未建立（请查看日志）}"
    exit 1
    ;;
disconnect)
    pid=$(python3 -c "import json;print(json.load(open('${STATUS_FILE}')).get('pid',0))" 2>/dev/null)
    if [ -n "${pid}" ] && [ "${pid}" -gt 0 ]; then
        kill "${pid}" 2>/dev/null
        sleep 2
        kill -9 "${pid}" 2>/dev/null || true
    fi
    cat > "${STATUS_FILE}" << EOF
{"name":"","pid":0,"connected":false,"remote":"","local_ip":"","remote_ip":"","auto_connect":false,"started_at":"","updated_at":"$(date +'%Y-%m-%d %H:%M:%S')"}
EOF
    chown nobody:nogroup "${STATUS_FILE}" 2>/dev/null || true
    echo "已断开"
    exit 0
    ;;
*)
    echo "用法: $0 connect <name> | disconnect"
    exit 1
    ;;
esac
