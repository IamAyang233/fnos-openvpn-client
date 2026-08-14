# OpenVPN 客户端（fnOS）

为飞牛私有云 (fnOS) 一键安装的 **OpenVPN 客户端** 应用（FPK 封装）。让飞牛作为 VPN 客户端连接任意 OpenVPN 服务器（公司 VPN、异地组网、服务商 VPN 等）。OpenVPN 二进制、Web 管理后端 (openvpn-client-web) 及运行库全部打包在 FPK 内，安装不联网，以原生进程方式跑在飞牛宿主机上，由应用中心统一管理启动 / 停止 / 状态。

## 功能

- **导入 .ovpn 配置**：粘贴文本（支持证书内嵌），可保存多套配置并在它们之间切换。
- **一键连接 / 断开**：实时显示连接状态（服务器地址、隧道 IP、连接时长）。
- **连接日志**：查看 openvpn 输出日志，连接 / 断开即时刷新。
- **开机自启**：NAS 重启后自动拨号；断线自动重连（设置页开关，可配最大重试次数，30s 退避，主动断开不触发）。
- **Web 管理界面**：经系统门户（统一网关）访问，复用 NAS 登录态，不对外暴露独立端口。
- **数据安全**：删除配置时连带清除同名 `.auth` 凭据文件避免密码残留泄露；数据持久化到应用数据卷，升级或重装不丢失。
- **Bug 反馈**：关于页内置「反馈」入口，可勾选上报日志；附交流 QQ 群入口。

## 适用前提

- **目标服务器**：需可访问目标 OpenVPN 服务器（公网 IP / 域名 / 内网均可）。
- **权限**：应用以 root 启动以创建 TUN 设备并加载防火墙规则（与 OpenVPN 服务端应用相同的官方许可场景）；Web 管理后端降权 `nobody` 运行，仅监听本机回环并经统一网关对外。
- **数据卷兜底**：框架未注入 `TRIM_PKGVAR` 且路径自推断 / 卷扫描均失败时，兜底定位主数据卷 vol1，兼容 vol1 / vol2 / vol3 任意部署。

## 安装

1. 飞牛 OS → 应用中心 → 「手动安装」→ 选择对应架构的安装包：
   - `openvpn-client_0.1.10_x86.fpk`（x86，如 N 系列）
   - `openvpn-client_0.1.10_arm.fpk`（ARM，如 F 系列）
2. 安装完成后应用中心显示「运行中」，桌面出现应用图标。
3. 点击图标经统一网关打开 Web 管理界面，粘贴 `.ovpn` 配置即可连接。

## 从源码构建

```bash
# 1. 编译 Web 管理后端（Go 1.23+，//go:embed 内嵌前端）
cd openvpn-client-src && go build -o ../fnos/app/bin/openvpn-client-web . && cd ..

# 2. 打包 x86 fpk
bash build.sh
# 产物：openvpn-client_0.1.10_x86.fpk

# 3. 打包 ARM fpk
FNOS_DIR=fnos_arm64_v4 bash build.sh
# 产物：openvpn-client_0.1.10_arm.fpk
```

> 说明：`build.sh` 负责把预编译二进制与 fpk 打包骨架打包成 `.fpk`；`openvpn-client-web` 由 `openvpn-client-src/main.go` 编译生成（亦可用环境变量 `WEB_BIN` 覆盖已编译二进制路径）。

## 目录结构

```
fnos/                       FPK 包内容（x86）
├── manifest               应用元数据（名称 / 版本 / 入口 / 描述）
├── config/                特权与资源配置
├── cmd/service-setup      安装后服务配置
├── ui/                    桌面入口（统一网关 /app/openvpn-client）
├── wizard/uninstall       卸载向导（可选保留数据）
├── app/
│   ├── bin/               openvpn / openvpn-client-web 后端 / 脚本
│   └── lib/               运行库（.so）
└── ICON.PNG / ICON_256.PNG  应用图标
fnos_arm64_v4/              ARM 架构同构打包骨架
openvpn-client-src/         Web 管理后端 Go 源码（//go:embed 内嵌前端）
├── main.go                连接 / 断开 / 状态 / 日志 / 设置
├── go.mod / go.sum
└── templates/             前端（index.html + css/js）
build.sh                   打包脚本
```

## 许可

OpenVPN 为 GPLv2（Community Edition）。本 FPK 由 Panda（www.aykeji.cn）打包发布。
