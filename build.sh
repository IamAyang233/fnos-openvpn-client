#!/bin/bash
# OpenVPN 客户端 FPK 打包脚本
# 用法：
#   ./build.sh                            # 打包 x86（fnos/）
#   FNOS_DIR=fnos_arm64_v4 ./build.sh     # 打包 ARM（fnos_arm64_v4/）
# Go 后端 + openvpn 二进制需先编译好放对应目录（见 build.sh 注释），本脚本只组装。
set -e
cd "$(dirname "$0")"

FNOS_DIR="${FNOS_DIR:-fnos}"
case "$FNOS_DIR" in *arm*) ARCH_TAG="arm" ;; *) ARCH_TAG="x86" ;; esac
APPNAME="openvpn-client"; VERSION="0.1.8"
OUT="${OUT:-${APPNAME}_${VERSION}_${ARCH_TAG}.fpk}"
[ -d "$FNOS_DIR" ] || { echo "缺少 $FNOS_DIR/"; exit 1; }

echo "==> 组装 app.tgz（$FNOS_DIR → $ARCH_TAG）"
WORK=".build_${ARCH_TAG}_$(date +%s)"
rm -rf "$WORK" 2>/dev/null || true   # safe-delete 沙箱拦截时忽略
mkdir -p "$WORK/app/bin" "$WORK/app/lib" "$WORK/app/ui/images" "$WORK/cmd"

# 二进制 + 脚本（openvpn 客户端 + web 后端 + helper + supervisor）
# WEB_BIN 可覆盖：当 fnos/app/bin/openvpn-client-web 被占用无法覆盖时，
# 用新编译产物（如 openvpn-client-src/web-new-amd64）直接打包。
WEB_BIN="${WEB_BIN:-$FNOS_DIR/app/bin/openvpn-client-web}"
cp "$FNOS_DIR/app/bin/openvpn"                 "$WORK/app/bin/"
cp "$WEB_BIN"                                 "$WORK/app/bin/openvpn-client-web"
cp "$FNOS_DIR/app/bin/ovpn-client-helper.sh"   "$WORK/app/bin/"
cp "$FNOS_DIR/app/bin/openvpn-client-supervisor.sh" "$WORK/app/bin/"
# 桌面入口（端口模式）
cp "$FNOS_DIR/app/lib/"* "$WORK/app/lib/" 2>/dev/null || true
cp "$FNOS_DIR/ui/config" "$WORK/app/ui/config"
# 注意：index.html 已落到 $FNOS_DIR/app/ui/（不再依赖外部 openvpn-client-src/），
# 由 build.sh 同目录的源树提供，x86/arm 各自携带，避免外部目录缺失导致打包中断。
cp "$FNOS_DIR/app/ui/index.html" "$WORK/app/ui/index.html"
cp "$FNOS_DIR/ICON.PNG"  "$WORK/app/ui/images/icon_64.png"
cp "$FNOS_DIR/ICON_256.PNG" "$WORK/app/ui/images/icon_256.png"

# cmd 生命周期骨架（与 bililive-fpk 相同：common + main + installer + 8 hooks + service-setup）
cat > "$WORK/cmd/common" << 'SHARED_COMMON'
#!/bin/bash
MV="/bin/mv -f"; RM="/bin/rm -rf"; CP="/bin/cp -rfp"; MKDIR="/bin/mkdir -p"
LN="/bin/ln -nsf"; RSYNC="/bin/rsync -avh"; TAR="/bin/tar"
if [ -z "${TRIM_PKGVAR:-}" ]; then echo "ERROR: TRIM_PKGVAR 未设置" >&2; exit 1; fi
case "${TRIM_PKGVAR}" in /vol*) ;; *) echo "ERROR: TRIM_PKGVAR=${TRIM_PKGVAR} 不在数据卷上" >&2; exit 1 ;; esac
[ -d "${TRIM_PKGVAR}" ] || /bin/mkdir -p "${TRIM_PKGVAR}" 2>/dev/null || true
INST_LOG="/var/log/apps/${TRIM_APPNAME}.log"
LOG_FILE="${TRIM_PKGVAR}/${TRIM_APPNAME}.log"
PID_FILE="${TRIM_PKGVAR}/${TRIM_APPNAME}.pid"
SVC_WAIT_TIMEOUT=15; SVC_CWD="${TRIM_PKGVAR}"; SVC_BACKGROUND=y; SVC_WRITE_PID=y; SVC_QUIET=y
DOCKER_NAME=""; DNAME="${TRIM_APPNAME}"; SVC_NO_REDIRECT=""
OUT=/dev/null; [ -z "${SVC_NO_REDIRECT}" ] && OUT="${LOG_FILE}"
error_exit() { echo "ERROR: $1" >&2; exit 1; }
install_log() { local _msg_="$@"; if [ -z "${_msg_}" ]; then while IFS=$'\n' read -r line; do install_log "${line}"; done; else echo -e "$(date +'%Y/%m/%d %H:%M:%S')\t${_msg_}" 1>&2; fi; }
call_func() { FUNC=$1; if type "${FUNC}" 2>/dev/null | grep -q 'function' 2>/dev/null; then echo "Begin ${FUNC}" >> ${LOG_FILE}; eval ${FUNC} >> ${LOG_FILE} 2>&1; echo "End ${FUNC}" >> ${LOG_FILE}; fi; }
log_step() { install_log "===> Step $1. STATUS=${TRIM_APP_STATUS}"; }
start_daemon() { if [ -n "$DOCKER_NAME" ]; then return; fi; call_func "service_prestart"; i=0; date >> ${LOG_FILE}; printf "%s" "$SERVICE_COMMAND" | while read -r service || [ -n "$service" ]; do i=$((i + 1)); echo "Starting ${DNAME} command ${service}" >> ${LOG_FILE}; [ -n "${SVC_CWD}" ] && cd ${SVC_CWD}; ${service} >> ${OUT} 2>&1 & disown; if [ -n "${SVC_WRITE_PID}" -a -n "${SVC_BACKGROUND}" ]; then [ $i -eq 1 ] && printf "%s" "$!" > ${PID_FILE} || printf "\n%s" "$!" >> ${PID_FILE}; fi; done; }
stop_daemon() { if [ -n "$DOCKER_NAME" ]; then return; fi; if [ -n "${PID_FILE}" -a -r "${PID_FILE}" ]; then for pid in $(cat "${PID_FILE}"); do [ -z "$pid" ] && continue; kill -TERM ${pid} >> ${LOG_FILE} 2>&1; sleep 2; kill -KILL ${pid} >> ${LOG_FILE} 2>&1 || true; done; rm -f "${PID_FILE}"; fi; }
daemon_status() { if [ -n "$DOCKER_NAME" ]; then return; fi; pid_list=$(cat ${PID_FILE} 2>/dev/null); [ -n "${pid_list}" ] || return 1; status=0; for pid in ${pid_list}; do kill -0 ${pid} > /dev/null 2>&1; status=$((status + $?)); done; [ $status -ne 0 ] && { rm -f "${PID_FILE}"; return 1; } || return 0; }
install_init()      { log_step "install_init"; call_func "service_preinst"; exit 0; }
install_callback()  { log_step "install_callback"; call_func "service_postinst"; exit 0; }
uninstall_init()    { log_step "uninstall_init"; stop_daemon; call_func "service_preuninst"; exit 0; }
uninstall_callback(){ log_step "uninstall_callback"; call_func "service_postuninst"; if [ "$wizard_delete_data" = "yes" ]; then find ${TRIM_PKGVAR} -mindepth 1 -delete -print | install_log; fi; exit 0; }
upgrade_init()      { log_step "upgrade_init"; stop_daemon; call_func "service_preupgrade"; call_func "service_save"; exit 0; }
upgrade_callback()  { log_step "upgrade_callback"; call_func "service_restore"; call_func "service_postupgrade"; exit 0; }
config_init()       { log_step "config_init"; call_func "service_preconfig"; exit 0; }
config_callback()   { log_step "config_callback"; call_func "service_postconfig"; exit 0; }
SHARED_COMMON

cat > "$WORK/cmd/main" << 'EOF'
#!/bin/bash
COMMON=$(dirname $0)"/common"; [ -r "${COMMON}" ] && . "${COMMON}"
SVC_SETUP=$(dirname $0)"/service-setup"; [ -r "${SVC_SETUP}" ] && . "${SVC_SETUP}"
case "$1" in
  start) if daemon_status; then exit 0; else start_daemon; exit $?; fi ;;
  stop) stop_daemon; exit 0 ;;
  status) if daemon_status; then echo "${DNAME} is running"; exit 0; else echo "${DNAME} is not running"; exit 3; fi ;;
  log) LINES="${2:-100}"; [ -f "${LOG_FILE}" ] && tail -n "$LINES" "${LOG_FILE}" || echo "No log"; exit 0 ;;
  *) exit 1 ;;
esac
EOF

cat > "$WORK/cmd/installer" << 'EOF'
#!/bin/bash
COMMON=$(dirname $0)"/common"; [ -r "${COMMON}" ] && . "${COMMON}"
SVC_SETUP=$(dirname $0)"/service-setup"; [ -r "${SVC_SETUP}" ] && . "${SVC_SETUP}"
case "$1" in
  install_init) install_init ;; install_callback) install_callback ;;
  uninstall_init) uninstall_init ;; uninstall_callback) uninstall_callback ;;
  upgrade_init) upgrade_init ;; upgrade_callback) upgrade_callback ;;
  config_init) config_init ;; config_callback) config_callback ;;
  *) exit 1 ;;
esac
EOF

for hook in install_init install_callback uninstall_init uninstall_callback upgrade_init upgrade_callback config_init config_callback; do
cat > "$WORK/cmd/$hook" << HOOK_EOF
#!/bin/bash
COMMON=\$(dirname \$0)"/common"; [ -r "\${COMMON}" ] && . "\${COMMON}"
SVC_SETUP=\$(dirname \$0)"/service-setup"; [ -r "\${SVC_SETUP}" ] && . "\${SVC_SETUP}"
${hook}
HOOK_EOF
done

cp "$FNOS_DIR/cmd/service-setup" "$WORK/cmd/service-setup"
chmod +x "$WORK/cmd/"* 2>/dev/null || true
tar czf "$WORK/app.tgz" -C "$WORK/app" .

echo "==> 打 fpk：$OUT"
rm -f "$OUT" 2>/dev/null || true
cp -r "$FNOS_DIR/config" "$WORK/"
# manifest（支持 MANIFEST 覆盖变量；无论源文件名都输出为 manifest）
# 默认用干净的 .manifest_v017（0.1.7，含正确 platform）；旧 manifest 被沙箱锁定时用它。
cp "${MANIFEST:-$FNOS_DIR/.manifest_v017}" "$WORK/manifest"
cp -rf "$FNOS_DIR/wizard" "$WORK/" 2>/dev/null || true
# 网关模式（ui/config 用 gatewaySocket/gatewayPrefix）无需端口转发 .sc，
# 故不再打包 openvpn-client.sc / 不在 resource 声明 port-config（P2 #1 端口残留清理）。
cp "$FNOS_DIR/ICON.PNG" "$FNOS_DIR/ICON_256.PNG" "$WORK/"
( cd "$WORK" && tar czf "../$OUT" manifest config cmd wizard ICON.PNG ICON_256.PNG app.tgz )

rm -rf "$WORK" 2>/dev/null || true
echo "==> 完成：$(ls -la "$OUT" | awk '{print $5, $9}')"
echo "    安装: 飞牛OS → 应用中心 → 手动安装 → 选择 $OUT"
