// OpenVPN 客户端 Web 管理后端（fnOS FPK）
// 功能：导入 .ovpn 配置、连接/断开、状态显示、日志、开机自启。
// 架构：web 进程降权 nobody 运行（端口 18081），连接/断开经 sudoers 白名单调
// ovpn-client-helper.sh（root 建 TUN 启动 openvpn 客户端进程）。
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var version = "0.1.10"

//go:embed templates
var embedFS embed.FS

var (
	ovData     string // 数据目录（@appdata/openvpn-client）
	etcDir     string // etc/
	confDir    string // configs/
	statusFP   string // 连接状态文件（运行态，helper 每次连接/断开整体重写）
	settingsFP string // 设置文件（auto_connect / auto_reconnect / reconnect_max，与运行态分离持久化）
	logFP      string // 客户端日志
	statFP     string // openvpn --status 流量统计文件
	helper     string // ovpn-client-helper.sh 绝对路径
)

func initPaths() {
	// TRIM_PKGVAR 由 fnOS 注入；本地开发兜底
	base := os.Getenv("TRIM_PKGVAR")
	if base == "" {
		base = "/var/apps/openvpn-client/var"
	}
	ovData = filepath.Join(base, "etc")
	etcDir = ovData
	confDir = filepath.Join(ovData, "configs")
	statusFP = filepath.Join(ovData, "status.json")
	settingsFP = filepath.Join(ovData, "settings.json")
	logFP = filepath.Join(ovData, "client.log")
	statFP = filepath.Join(ovData, "openvpn-status.log") // helper 启动 openvpn 时 --status 输出
	dest := os.Getenv("TRIM_APPDEST")
	if dest == "" {
		dest = "/var/apps/openvpn-client/target"
	}
	helper = filepath.Join(dest, "bin", "ovpn-client-helper.sh")
	for _, d := range []string{etcDir, confDir} {
		os.MkdirAll(d, 0755)
	}
}

// ---------- 状态读写 ----------

type ConnStatus struct {
	Name          string `json:"name"`           // 当前配置名（空=未连接）
	PID           int    `json:"pid"`            // openvpn 客户端进程 PID
	Connected     bool   `json:"connected"`      // 是否已连接（隧道建立）
	Remote        string `json:"remote"`         // 服务器地址
	LocalIP       string `json:"local_ip"`       // 隧道本端 IP
	RemoteIP      string `json:"remote_ip"`      // 隧道对端 IP
	AutoConnect   bool   `json:"auto_connect"`   // 开机自启
	WantConnected bool   `json:"want_connected"` // 当前期望保持连接（连接成功=true，主动断开=false）
	AutoReconnect bool   `json:"auto_reconnect"` // 断线自动重连开关
	ReconnectMax  int    `json:"reconnect_max"`  // 最大重连次数（0=无限）
	StartedAt     string `json:"started_at"`     // 启动时间
	UpdatedAt     string `json:"updated_at"`     // 状态更新
	RxBytes       int64  `json:"rx_bytes"`       // 下行累计字节（TUN/TAP read）
	TxBytes       int64  `json:"tx_bytes"`       // 上行累计字节（TUN/TAP write）
	RxRate        int64  `json:"rx_rate"`        // 下行速率 bytes/s
	TxRate        int64  `json:"tx_rate"`        // 上行速率 bytes/s
}

// 流量速率差分缓存（web 进程常驻内存，按轮询间隔算速率）
var (
	lastRx, lastTx int64
	lastStatTs     time.Time
)

// TrafficSample 单次速率采样（前端画实时曲线用）
type TrafficSample struct {
	T  int64 `json:"t"`  // unix 秒
	Rx int64 `json:"rx"` // 下行速率 bytes/s
	Tx int64 `json:"tx"` // 上行速率 bytes/s
}

// trafficHistory 滑动窗口（最近 ~5 分钟，按前端轮询间隔采样）
var trafficHistory []TrafficSample

// trafficMu 保护 trafficHistory / lastRx / lastTx / lastStatTs：
// enrichTraffic 会被 apiBootstrap 与 apiStatus 两个并发请求同时调用，
// 无锁并发 append/reslice 是真实 data race（go -race 会报，高并发可能 panic）。
var trafficMu sync.Mutex

const trafficMaxSamples = 300

// 自动重连：后台 watcher 状态（want_connected 区分主动断开 vs 异常断线）
var (
	reconnectFails int
	lastReconnect  time.Time
	reconnectMu    sync.Mutex
)

const reconnectBackoff = 30 * time.Second

// Settings 用户设置（独立于 status.json 持久化——helper 连接/断开时整体重写
// status.json 且不含设置字段，若设置存 status.json 会被冲掉导致开关失效）
type Settings struct {
	AutoConnect   bool `json:"auto_connect"`
	AutoReconnect bool `json:"auto_reconnect"`
	ReconnectMax  int  `json:"reconnect_max"`
}

func loadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(settingsFP); err == nil {
		json.Unmarshal(b, &s)
		return s
	}
	// 一次性迁移：v0.1.6 及之前 auto_connect 存在 status.json（helper 重写前可能残留）
	if b, err := os.ReadFile(statusFP); err == nil {
		var st ConnStatus
		if json.Unmarshal(b, &st) == nil {
			s.AutoConnect = st.AutoConnect
			saveSettings(s)
		}
	}
	return s
}

func saveSettings(s Settings) {
	b, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(settingsFP, b, 0644)
}

// applySettings 把持久化设置合并进运行态 ConnStatus（status.json 里的 auto_* 字段不可信）
func applySettings(st *ConnStatus) {
	s := loadSettings()
	st.AutoConnect = s.AutoConnect
	st.AutoReconnect = s.AutoReconnect
	st.ReconnectMax = s.ReconnectMax
}

func loadStatus() ConnStatus {
	var st ConnStatus
	if b, err := os.ReadFile(statusFP); err == nil {
		json.Unmarshal(b, &st)
	}
	return st
}

func saveStatus(st ConnStatus) {
	b, _ := json.MarshalIndent(st, "", "  ")
	os.WriteFile(statusFP, b, 0644)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// 用 /proc/<pid> 存在性判断：web(nobody) 对 root 进程发 signal 0 会 EPERM（权限拒绝），
	// 导致 root 跑的 openvpn 客户端被误判为已退出、状态被清空。读 /proc 目录不需要信号权限。
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err == nil {
		return true
	}
	return false
}

// ---------- helper 调用（sudoers 白名单）----------

func runHelper(args ...string) (string, error) {
	cmd := exec.Command("sudo", append([]string{"-n", helper}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ---------- API ----------

func apiBootstrap(c *gin.Context) {
	st := loadStatus()
	applySettings(&st)
	// 进程存活但隧道状态过期 → 标记断开（保留 Name 供自动重连找回最后连接配置）
	if st.PID > 0 && !pidAlive(st.PID) {
		st.PID = 0
		st.Connected = false
		saveStatus(st)
	}
	enrichTraffic(&st)
	names := listConfigs()
	// 自动重连检测：启用 + 期望连接 + 当前未连接 + 上次重连间隔已过 → 触发重连
	if st.AutoReconnect && st.WantConnected && !st.Connected && len(names) > 0 {
		reconnectMu.Lock()
		shouldReconnect := false
		if !lastReconnect.IsZero() && time.Since(lastReconnect) < reconnectBackoff {
			// 退避中，不触发
		} else if st.ReconnectMax > 0 && reconnectFails >= st.ReconnectMax {
			// 已达最大次数，清空期望避免死循环
			st.WantConnected = false
			saveStatus(st)
		} else {
			shouldReconnect = true
		}
		reconnectMu.Unlock()
		if shouldReconnect {
			// 优先重连最后连接的配置（st.Name 在进程死亡时保留），否则取第一个
			name := st.Name
			if name == "" {
				name = names[0]
			}
			go tryReconnect(name)
		}
	}
	c.JSON(200, gin.H{
		"version":        version,
		"status":         st,
		"traffic":        snapshotTraffic(),
		"configs":        names,
		"auto_connect":   st.AutoConnect,
		"auto_reconnect": st.AutoReconnect,
		"reconnect_max":  st.ReconnectMax,
	})
}

// tryReconnect 异步重连（由 apiBootstrap 触发的 goroutine）。
// 退避保护：锁内检查+更新时间戳，多个 bootstrap 并发触发时仅一个生效。
// 失败/成功均计入 reconnectFails（达 ReconnectMax 后 bootstrap 停止触发）。
func tryReconnect(name string) {
	reconnectMu.Lock()
	if !lastReconnect.IsZero() && time.Since(lastReconnect) < reconnectBackoff {
		reconnectMu.Unlock()
		return // 退避中（bootstrap 已检查过，双保险）
	}
	reconnectFails++
	lastReconnect = time.Now()
	reconnectMu.Unlock()
	fmt.Printf("[auto-reconnect] 第 %d 次尝试重连 %s ...\n", reconnectFails, name)
	out, err := runHelper("connect", name)
	if err != nil {
		fmt.Printf("[auto-reconnect] 重连 %s 失败: %s\n", name, strings.TrimSpace(out))
		return
	}
	// 重连成功：helper 已写 status.json（name/pid/connected=true）；
	// 补回 WantConnected（helper 整体重写会把该字段冲掉，否则下次断线不再触发重连）
	st := loadStatus()
	st.WantConnected = true
	reconnectMu.Lock()
	reconnectFails = 0
	reconnectMu.Unlock()
	saveStatus(st)
	fmt.Printf("[auto-reconnect] %s 重连成功\n", name)
}

func apiConfigs(c *gin.Context) {
	c.JSON(200, gin.H{"configs": listConfigs()})
}

// GET /api/configs/:name —— 配置详情（编辑用：返回内容 + 是否已存凭据 + 用户名）
func apiConfigDetail(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "缺少名称"})
		return
	}
	fp := filepath.Join(confDir, name+".ovpn")
	b, err := os.ReadFile(fp)
	if err != nil {
		c.JSON(404, gin.H{"error": "配置不存在"})
		return
	}
	username := ""
	af := filepath.Join(confDir, name+".auth")
	if ab, err := os.ReadFile(af); err == nil {
		lines := strings.SplitN(string(ab), "\n", 2)
		username = lines[0]
	}
	c.JSON(200, gin.H{"name": name, "content": string(b), "username": username})
}

func listConfigs() []string {
	ents, _ := os.ReadDir(confDir)
	names := []string{}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".ovpn") {
			names = append(names, strings.TrimSuffix(e.Name(), ".ovpn"))
		}
	}
	sort.Strings(names)
	return names
}

func apiImport(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Content  string `json:"content"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	content := req.Content
	if name == "" || strings.TrimSpace(content) == "" {
		c.JSON(400, gin.H{"error": "名称和配置内容不能为空"})
		return
	}
	// 名称白名单：字母数字-_。
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			c.JSON(400, gin.H{"error": "名称只能包含字母数字-_."})
			return
		}
	}
	// 防注入：拒绝配置里嵌 command/up/down 等危险指令（v1 简化，仅拦截明显危险项）
	for _, bad := range []string{"up ", "down ", "script-security 2", "learn-address", "ipchange"} {
		if strings.Contains(strings.ToLower(content), bad) {
			c.JSON(400, gin.H{"error": "配置包含受限指令（" + bad + "），请移除后重试"})
			return
		}
	}
	fp := filepath.Join(confDir, name+".ovpn")
	// 隧道账户密码（可选）：服务器要求认证时填写。
	// 保存到 <name>.auth（用户名\n密码），并在 .ovpn 中注入 auth-user-pass <auth文件>。
	if req.Username != "" {
		authFile := filepath.Join(confDir, name+".auth")
		if err := os.WriteFile(authFile, []byte(req.Username+"\n"+req.Password+"\n"), 0600); err != nil {
			c.JSON(500, gin.H{"error": "保存凭据失败: " + err.Error()})
			return
		}
		if strings.Contains(strings.ToLower(content), "auth-user-pass") {
			content = regexpAuthUserPass.ReplaceAllString(content, "auth-user-pass "+authFile)
		} else {
			content += "\nauth-user-pass " + authFile + "\n"
		}
	}
	if err := os.WriteFile(fp, []byte(content), 0600); err != nil {
		c.JSON(500, gin.H{"error": "保存失败: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func apiDelete(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "缺少名称"})
		return
	}
	st := loadStatus()
	if st.Name == name && st.Connected {
		c.JSON(400, gin.H{"error": "请先断开该配置"})
		return
	}
	os.Remove(filepath.Join(confDir, name+".ovpn"))
	// 连带删除凭据文件（name 经白名单校验，不含 / 或 ..，无路径穿越风险）
	os.Remove(filepath.Join(confDir, name+".auth"))
	c.JSON(200, gin.H{"ok": true})
}

func apiConnect(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(400, gin.H{"error": "缺少配置名"})
		return
	}
	fp := filepath.Join(confDir, req.Name+".ovpn")
	if _, err := os.Stat(fp); err != nil {
		c.JSON(404, gin.H{"error": "配置不存在"})
		return
	}
	st := loadStatus()
	if st.Connected {
		c.JSON(400, gin.H{"error": "已有一个连接（" + st.Name + "），请先断开"})
		return
	}
	out, err := runHelper("connect", req.Name)
	if err != nil {
		reason := strings.TrimSpace(out)
		if reason == "" {
			reason = "连接未建立（未知原因，请查看日志）"
		}
		c.JSON(500, gin.H{"error": "连接失败: " + reason})
		return
	}
	// 标记期望保持连接（供自动重连 watcher 使用），并重置失败计数
	st = loadStatus()
	st.WantConnected = true
	reconnectMu.Lock()
	reconnectFails = 0
	reconnectMu.Unlock()
	saveStatus(st)
	c.JSON(200, gin.H{"ok": true, "status": st})
}

func apiDisconnect(c *gin.Context) {
	out, err := runHelper("disconnect")
	if err != nil {
		c.JSON(500, gin.H{"error": "断开失败: " + strings.TrimSpace(out)})
		return
	}
	// 主动断开：清除期望连接意图，避免 watcher 立即重连
	st := loadStatus()
	st.WantConnected = false
	reconnectMu.Lock()
	reconnectFails = 0
	reconnectMu.Unlock()
	saveStatus(st)
	c.JSON(200, gin.H{"ok": true})
}

// enrichTraffic 读取 openvpn --status 文件，填充累计字节并差分计算速率。
// 仅 connected 时有效；断开或重连（累计值回落）时速率归零。
func enrichTraffic(st *ConnStatus) {
	trafficMu.Lock()
	defer trafficMu.Unlock()
	if !st.Connected {
		st.RxBytes, st.TxBytes, st.RxRate, st.TxRate = 0, 0, 0, 0
		lastRx, lastTx, lastStatTs = 0, 0, time.Time{}
		trafficHistory = nil
		return
	}
	b, err := os.ReadFile(statFP)
	if err != nil {
		return
	}
	var rx, tx int64
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "TUN/TAP read bytes,"):
			fmt.Sscanf(strings.TrimPrefix(line, "TUN/TAP read bytes,"), "%d", &rx)
		case strings.HasPrefix(line, "TUN/TAP write bytes,"):
			fmt.Sscanf(strings.TrimPrefix(line, "TUN/TAP write bytes,"), "%d", &tx)
		}
	}
	now := time.Now()
	st.RxBytes, st.TxBytes = rx, tx
	// 差分速率：累计值应单调增；若回落（重连）或无上次样本则记 0
	if !lastStatTs.IsZero() && rx >= lastRx && tx >= lastTx {
		dt := now.Sub(lastStatTs).Seconds()
		if dt > 0 {
			st.RxRate = int64(float64(rx-lastRx) / dt)
			st.TxRate = int64(float64(tx-lastTx) / dt)
		}
	}
	lastRx, lastTx, lastStatTs = rx, tx, now
	// 采样到滑动窗口（前端画实时曲线）
	trafficHistory = append(trafficHistory, TrafficSample{T: now.Unix(), Rx: st.RxRate, Tx: st.TxRate})
	if len(trafficHistory) > trafficMaxSamples {
		trafficHistory = trafficHistory[len(trafficHistory)-trafficMaxSamples:]
	}
}

// snapshotTraffic 在锁内拷贝当前流量窗口，供 handler 序列化返回。
// 不可直接返回全局 trafficHistory：enrichTraffic 会在另一请求中并发 append/reslice，
// 序列化读到的 slice header 可能正在被改写（data race）。
func snapshotTraffic() []TrafficSample {
	trafficMu.Lock()
	defer trafficMu.Unlock()
	s := make([]TrafficSample, len(trafficHistory))
	copy(s, trafficHistory)
	return s
}

func apiStatus(c *gin.Context) {
	st := loadStatus()
	applySettings(&st)
	// 刷新连接信息（helper 每次连接后更新 status.json 里的隧道信息）
	if st.PID > 0 && !pidAlive(st.PID) {
		st.PID = 0
		st.Connected = false
		st.Name = ""
		saveStatus(st)
	}
	enrichTraffic(&st)
	c.JSON(200, gin.H{"status": st, "traffic": snapshotTraffic()})
}

func apiLog(c *gin.Context) {
	lines := 200
	n := 0
	fmt.Sscanf(c.Query("lines"), "%d", &n)
	if n > 0 {
		lines = n
	}
	b, err := os.ReadFile(logFP)
	if err != nil {
		c.JSON(200, gin.H{"log": ""})
		return
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	c.JSON(200, gin.H{"log": strings.Join(all, "\n")})
}

func apiAuto(c *gin.Context) {
	var req struct {
		Enable        bool `json:"enable"`
		AutoReconnect bool `json:"auto_reconnect"`
		ReconnectMax  int  `json:"reconnect_max"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.ReconnectMax < 0 {
		req.ReconnectMax = 0
	}
	s := loadSettings()
	s.AutoConnect = req.Enable
	s.AutoReconnect = req.AutoReconnect
	s.ReconnectMax = req.ReconnectMax
	saveSettings(s)
	// 重连策略未启用时清空重连计数，避免重新开启后沿用旧计数直接判超限
	if !s.AutoReconnect {
		reconnectMu.Lock()
		reconnectFails = 0
		reconnectMu.Unlock()
	}
	c.JSON(200, gin.H{"ok": true, "auto_connect": s.AutoConnect, "auto_reconnect": s.AutoReconnect, "reconnect_max": s.ReconnectMax})
}

// 简单认证：与服务器版一致，用固定 token（v1 内网管理界面，后续可加登录）
// ---------- 关于：检测更新 + Bug 反馈（复用 panda 推送更新机制） ----------

const pandaAPI = "https://www.aykeji.cn" // panda 主页公网域名

var regexpAuthUserPass = regexp.MustCompile(`(?im)^auth-user-pass\s*\S*\s*$`)

// GET /api/about/update —— 转发 panda 通用更新查询（/api/app-update/openvpn-client）
func apiAboutUpdate(c *gin.Context) {
	u := pandaAPI + "/api/app-update/openvpn-client?current=" + version
	resp, err := http.Get(u)
	if err != nil {
		c.JSON(200, gin.H{"ok": false, "error": "无法连接更新服务: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		c.JSON(200, gin.H{"ok": false, "error": "更新服务响应异常"})
		return
	}
	out["current"] = version // 覆盖为自身版本
	c.JSON(200, out)
}

// POST /api/about/feedback —— 收集诊断信息（版本/配置/日志尾部）转发 panda 通用反馈
func apiAboutFeedback(c *gin.Context) {
	var req struct {
		Category    string `json:"category"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Contact     string `json:"contact"`
		IncludeLogs bool   `json:"include_logs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Title == "" {
		c.JSON(400, gin.H{"error": "标题不能为空"})
		return
	}
	payload := map[string]string{
		"app":         "openvpn-client",
		"version":     version,
		"category":    req.Category,
		"title":       req.Title,
		"description": req.Description,
		"contact":     req.Contact,
		"logs":        collectClientLogs(req.IncludeLogs),
	}
	pb, _ := json.Marshal(payload)
	req2, err := http.NewRequest("POST", pandaAPI+"/api/app-feedback/openvpn-client", bytes.NewReader(pb))
	if err != nil {
		c.JSON(500, gin.H{"error": "构造请求失败"})
		return
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Feedback-Token", "fnos-openvpn-client-feedback")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		c.JSON(200, gin.H{"ok": false, "error": "无法连接反馈服务: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	json.Unmarshal(body, &out)
	c.JSON(200, out)
}

// collectClientLogs 收集诊断信息：版本 + 当前状态 + 配置列表 + 日志尾部
func collectClientLogs(include bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "=== openvpn-client 诊断信息 ===\n")
	fmt.Fprintf(&sb, "版本: %s\n", version)
	st := loadStatus()
	fmt.Fprintf(&sb, "连接: %s (pid=%d connected=%v auto=%v)\n", st.Name, st.PID, st.Connected, st.AutoConnect)
	names := listConfigs()
	fmt.Fprintf(&sb, "配置: %d 个 → %s\n\n", len(names), strings.Join(names, ", "))
	if !include {
		return sb.String()
	}
	lines := readTail(logFP, 100)
	fmt.Fprintf(&sb, "--- client.log (尾 %d 行) ---\n%s\n", len(lines), strings.Join(lines, "\n"))
	return sb.String()
}

func readTail(fp string, n int) []string {
	b, err := os.ReadFile(fp)
	if err != nil {
		return []string{"(无此日志)"}
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := c.GetHeader("X-Client-Token")
		env := os.Getenv("OVPN_CLIENT_TOKEN")
		if env == "" {
			env = "fnos-openvpn-client"
		}
		if tok != env {
			c.JSON(401, gin.H{"error": "未授权"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// gwResponseWriter 在 3xx 重定向响应中，把 Location 的 "/" 前缀重写为 GATEWAY_PREFIX，
// 使浏览器在 fnOS 统一网关内正确跳转（网关模式与服务器版同实现）。
type gwResponseWriter struct {
	http.ResponseWriter
	prefix string
}

func (w *gwResponseWriter) WriteHeader(code int) {
	if w.prefix != "" && code >= 300 && code < 400 {
		if loc := w.Header().Get("Location"); loc != "" && strings.HasPrefix(loc, "/") {
			w.Header().Set("Location", w.prefix+loc)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func main() {
	initPaths()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// 静态资源（embed 前端）
	staticFS, _ := fs.Sub(embedFS, "templates/static")
	r.StaticFS("/static", http.FS(staticFS))

	api := r.Group("/api", authMiddleware())
	api.GET("/bootstrap", apiBootstrap)
	api.GET("/configs", apiConfigs)
	api.GET("/configs/:name", apiConfigDetail)
	api.POST("/import", apiImport)
	api.DELETE("/configs/:name", apiDelete)
	api.POST("/connect", apiConnect)
	api.POST("/disconnect", apiDisconnect)
	api.GET("/status", apiStatus)
	api.GET("/log", apiLog)
	api.POST("/auto", apiAuto)
	api.GET("/about/update", apiAboutUpdate)
	api.POST("/about/feedback", apiAboutFeedback)

	// 前端页面
	r.GET("/", func(c *gin.Context) {
		b, _ := fs.ReadFile(embedFS, "templates/index.html")
		c.Data(200, "text/html; charset=utf-8", b)
	})

	// ---------- 统一网关（v0.1.5，复刻 openvpn 服务器网关模式） ----------
	// 网关监听：SOCKET_PATH 非空时监听 unix socket，由 fnOS 统一网关经 /app/openvpn-client 转发。
	// TCP 仅回环监听（本地调试/回调），不再对外暴露 18081（防 LAN 直连绕过 NAS 登录态）。
	// 前置网关中间件：剥离 GATEWAY_PREFIX；响应 3xx 时把 Location 的 "/" 前缀重写为 GATEWAY_PREFIX。
	gwPrefix := os.Getenv("GATEWAY_PREFIX")
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if gwPrefix != "" {
			if req.URL.Path == gwPrefix {
				http.Redirect(w, req, gwPrefix+"/", 302)
				return
			}
			if strings.HasPrefix(req.URL.Path, gwPrefix) {
				req.URL.Path = strings.TrimPrefix(req.URL.Path, gwPrefix)
				if req.URL.Path == "" {
					req.URL.Path = "/"
				}
			}
		}
		rw := &gwResponseWriter{ResponseWriter: w, prefix: gwPrefix}
		r.ServeHTTP(rw, req)
	})

	// TCP 回环通道
	addr := os.Getenv("OVPN_CLIENT_BIND")
	if addr == "" {
		addr = "127.0.0.1:18081"
	}
	go func() {
		srv := &http.Server{Addr: addr, Handler: handler}
		fmt.Println("OpenVPN 客户端 v" + version + " TCP 回环监听 " + addr)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			fmt.Println("TCP listen error:", e.Error())
		}
	}()

	// 网关 unix socket 通道
	socketPath := os.Getenv("SOCKET_PATH")
	if socketPath != "" {
		os.Remove(socketPath)
		if dir := filepath.Dir(socketPath); dir != "" {
			os.MkdirAll(dir, 0755)
		}
		ul, e := net.Listen("unix", socketPath)
		if e != nil {
			fmt.Println("Unix socket listen error:", e.Error())
		} else {
			_ = os.Chmod(socketPath, 0666)
			fmt.Println("OpenVPN 客户端 v" + version + " 网关监听 " + socketPath)
			srv := &http.Server{Handler: handler}
			go func() {
				if se := srv.Serve(ul); se != nil && se != http.ErrServerClosed {
					fmt.Println("Unix socket serve error:", se.Error())
				}
			}()
			defer os.Remove(socketPath)
		}
	}

	select {}
}
