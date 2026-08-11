/* OpenVPN 客户端 —— 前端逻辑（与服务器共用 CSS/sprite，API 对接客户端后端） */
(function () {
  'use strict';

  var TOKEN = 'fnos-openvpn-client';
  // 统一网关 base path：页面经 /app/openvpn-client 访问时，所有 API 请求要带网关前缀
  // （否则 fetch('/api/...') 打到根路径，网关不匹配前缀 → 404 → 数据永远加载不出来）。
  var BASE = (function () {
    var m = (location.pathname || '').match(/^\/app\/[A-Za-z0-9_-]+/);
    return m ? m[0] : '';
  })();
  var $ = function (id) { return document.getElementById(id); };
  var esc = function (s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  };

  var toastTimer;
  function toast(msg) {
    var t = document.getElementById('toast');
    if (!t) return;
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { t.classList.remove('show'); }, 1800);
  }

  function norm(r) {
    return r.json().catch(function () { return { error: '响应异常' }; });
  }
  var api = {
    get: function (u) {
      return fetch(BASE + '/api' + u, { headers: { 'X-Client-Token': TOKEN } }).then(norm).catch(function () { return { error: '网络错误' }; });
    },
    post: function (u, b) {
      return fetch(BASE + '/api' + u, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Client-Token': TOKEN },
        body: JSON.stringify(b || {})
      }).then(norm).catch(function () { return { error: '网络错误' }; });
    },
    del: function (u) {
      return fetch(BASE + '/api' + u, { method: 'DELETE', headers: { 'X-Client-Token': TOKEN } }).then(norm).catch(function () { return { error: '网络错误' }; });
    }
  };

  var state = { status: null, configs: [], ver: '-' };

  /* ---------- 主题 ---------- */
  var themeBtn = $('themeBtn'), themeIcon = $('themeIcon');
  function setTheme(t) {
    document.documentElement.setAttribute('data-theme', t);
    var ic = t === 'dark' ? '#i-sun' : '#i-moon';
    if (themeIcon) themeIcon.querySelector('use').setAttribute('href', ic);
    if (themeBtn) themeBtn.setAttribute('aria-pressed', t === 'dark');
    try { localStorage.setItem('ovpn-theme', t); } catch (e) {}
  }
  (function initTheme() {
    var t = 'light';
    try { t = localStorage.getItem('ovpn-theme') || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'); } catch (e) {}
    setTheme(t);
  })();
  if (themeBtn) themeBtn.addEventListener('click', function () {
    setTheme(document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
  });

  /* ---------- 面板切换 ---------- */
  var TITLES = { dashboard: '连接', configs: '配置', logs: '日志', settings: '设置', about: '关于' };
  function showPanel(name) {
    document.querySelectorAll('.nav-item').forEach(function (n) { n.classList.toggle('active', n.dataset.screen === name); });
    document.querySelectorAll('[data-panel]').forEach(function (p) { p.hidden = p.dataset.panel !== name; });
    $('pageTitle').textContent = TITLES[name] || name;
    $('pageCrumb').textContent = 'OpenVPN 客户端 · ' + (TITLES[name] || name);
    if (name === 'dashboard') refresh();
    if (name === 'configs') refresh();
    if (name === 'logs') refreshLog();
    if (name === 'about') checkUpdate();   // 与服务端一致：进入关于页自动检查更新 + 显示日志
    if (window.innerWidth <= 920) closeDrawer();
  }
  document.querySelectorAll('.nav-item').forEach(function (a) {
    a.addEventListener('click', function (e) { e.preventDefault(); showPanel(a.dataset.screen); });
  });
  var app = $('app');
  var backdrop = $('drawerBackdrop');
  function openDrawer() { app.classList.remove('collapsed'); backdrop.hidden = false; }
  function closeDrawer() { if (window.innerWidth <= 920) { app.classList.add('collapsed'); backdrop.hidden = true; } }
  $('menuBtn').addEventListener('click', function () {
    if (window.innerWidth <= 920) { backdrop.hidden ? openDrawer() : closeDrawer(); }
    else { app.classList.toggle('collapsed'); }
  });
  backdrop.addEventListener('click', closeDrawer);
  if (window.innerWidth <= 920) app.classList.add('collapsed');
  $('importBtn').addEventListener('click', function () { showPanel('configs'); showImport(); });
  $('configImportBtn').addEventListener('click', showImport);
  $('refreshBtn').addEventListener('click', refresh);
  $('logRefreshBtn').addEventListener('click', refreshLog);

  var editOrigin = '';   // 编辑时的原配置名（空 = 导入新配置）
  function showImport() {
    editOrigin = '';
    $('inpName').value = ''; $('inpContent').value = ''; $('inpUser').value = ''; $('inpPass').value = '';
    $('fileHint').textContent = '或拖拽 .ovpn 文件到下方配置框';
    $('cfgDialogTitle').textContent = '导入 .ovpn 配置';
    $('importErr').textContent = '';
    $('cfgDialog').showModal();
    $('inpName').focus();
  }
  function closeDialog() { if ($('cfgDialog').open) $('cfgDialog').close(); }
  $('importCancelBtn').addEventListener('click', closeDialog);
  $('cfgDialogClose').addEventListener('click', closeDialog);
  // 点击弹窗外（backdrop）关闭
  var suppressDialogClickUntil = 0;   // 文件选择框关闭后浏览器补发的游离 click 屏蔽截止时间
  $('cfgDialog').addEventListener('click', function (e) {
    if (Date.now() < suppressDialogClickUntil) return;   // 吃掉“选择文件”后误触的游离 click，避免弹窗消失
    if (e.target === this) this.close();                 // 仅点击弹窗本身（背景）才关闭，点内部控件不关
  });
  $('importSaveBtn').addEventListener('click', doImport);

  /* ---------- 数据加载 ---------- */
  var fastPollTimer = null;
  function refresh() {
    api.get('/bootstrap').then(function (d) {
      if (d.error) return;
      state.status = d.status || {};
      state.configs = d.configs || [];
      state.traffic = d.traffic || [];
      state.ver = d.version || '-';
      var av = $('aboutVer'); if (av) av.textContent = state.ver;
      try { renderStatus(); renderConfigs(); } catch (e) { console.error(e); }
    }).catch(function () {});
  }
  // 连接后快轮询：每 2 秒刷新状态，直到状态变化或 30 秒超时（回落常规 8 秒轮询）
  function startFastPoll() {
    clearInterval(fastPollTimer);
    var n = 0;
    fastPollTimer = setInterval(function () {
      n++;
      refresh();
      if (n >= 15) clearInterval(fastPollTimer);
    }, 2000);
  }

  function formatBytes(n) {
    n = Number(n) || 0;
    if (n < 1024) return n + ' B';
    var u = ['KB', 'MB', 'GB', 'TB'];
    var i = -1;
    do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
    return n.toFixed(n < 10 ? 2 : 1) + ' ' + u[i];
  }
  function formatRate(n) { return formatBytes(n) + '/s'; }

  function renderStatus() {
    var st = state.status, on = !!(st && st.connected);
    var chip = $('connChip');
    chip.textContent = on ? '已连接' : '未连接';
    chip.className = 'chip ' + (on ? 'chip-ok' : '');
    $('connName').textContent = on
      ? ('已连接 ' + (st.name || '') + (st.remote ? ' → ' + st.remote : ''))
      : '当前未连接任何 VPN';
    $('connectBtn').disabled = !!on;
    $('disconnectBtn').disabled = !on;
    $('vName').textContent = on ? st.name : '-';
    $('vRemote').textContent = st.remote || '-';
    $('vLocal').textContent = st.local_ip || st.remote_ip || '-';
    $('vStart').textContent = st.started_at || '-';
    $('autoChk').checked = !!st.auto_connect;
    $('autoReconnectChk').checked = !!st.auto_reconnect;
    var rm = $('reconnectMax');
    if (rm && st.reconnect_max != null) rm.value = st.reconnect_max;
    syncReconnectField();
    // 流量曲线区：仅连接时显示
    var box = $('trafficBox');
    if (box) box.hidden = !on;
    if (on) {
      $('tRxRate').textContent = formatRate(st.rx_rate || 0);
      $('tTxRate').textContent = formatRate(st.tx_rate || 0);
      $('tRxTotal').textContent = formatBytes(st.rx_bytes || 0);
      $('tTxTotal').textContent = formatBytes(st.tx_bytes || 0);
      drawTraffic(state.traffic || []);
    }
  }

  // 用纯 SVG 画上下行两条实时速率面积/折线图（viewBox 600x120，preserveAspectRatio=none 自适应宽）
  function drawTraffic(samples) {
    var svg = $('tChart'); if (!svg) return;
    var W = 600, H = 120, pad = 4;
    if (!samples.length) {
      ['tAreaRx', 'tLineRx', 'tAreaTx', 'tLineTx'].forEach(function (id) { $(id).setAttribute('d', ''); });
      return;
    }
    var maxV = 1;
    samples.forEach(function (s) { maxV = Math.max(maxV, s.rx || 0, s.tx || 0); });
    // 给顶部留 15% 余量，避免峰值贴边
    maxV = maxV * 1.15;
    var n = samples.length;
    function xy(i, v) {
      var x = n === 1 ? W : (i / (n - 1)) * W;
      var y = H - pad - (v / maxV) * (H - pad * 2);
      return [x, y];
    }
    function build(sel) {
      var line = '', area = '';
      for (var i = 0; i < n; i++) {
        var p = xy(i, samples[i][sel]);
        line += (i === 0 ? 'M' : 'L') + p[0].toFixed(1) + ' ' + p[1].toFixed(1) + ' ';
      }
      area = 'M0 ' + H + ' ' + line.replace(/^M/, 'L') + 'L' + W + ' ' + H + ' Z';
      return { line: line.trim(), area: area };
    }
    var rx = build('rx'), tx = build('tx');
    $('tLineRx').setAttribute('d', rx.line);
    $('tAreaRx').setAttribute('d', rx.area);
    $('tLineTx').setAttribute('d', tx.line);
    $('tAreaTx').setAttribute('d', tx.area);
  }

  function renderConfigs() {
    var box = $('cfgList');
    if (!state.configs.length) {
      box.innerHTML = '<div class="hint">还没有配置，点「导入配置」添加第一个 .ovpn。</div>';
      return;
    }
    box.innerHTML = state.configs.map(function (n) {
      var st = state.status;
      var active = st && st.connected && st.name === n;
      var remote = active && st.remote ? st.remote : '';
      return '<div class="cfg-item' + (active ? ' active' : '') + '">' +
        '<span class="cfg-dot' + (active ? ' on' : '') + '"></span>' +
        '<div class="cfg-info">' +
        '<div class="cfg-name">' + esc(n) + (active ? ' <span class="chip chip-ok">已连接</span>' : '') + '</div>' +
        '<div class="cfg-remote' + (active ? '' : ' muted') + '">' + (active ? esc(remote) : '未连接') + '</div>' +
        '</div>' +
        '<div class="cfg-ops">' +
        (active
          ? '<button class="btn btn-sm btn-danger cfg-op" data-act="disconnect">断开</button>'
          : '<button class="btn btn-sm btn-primary cfg-op" data-act="connect" data-name="' + esc(n) + '">连接</button>') +
        '<button class="btn btn-sm cfg-op" data-act="edit" data-name="' + esc(n) + '"><svg class="icon icon-sm"><use href="#i-edit"/></svg>编辑</button>' +
        '<button class="btn btn-sm cfg-op" data-act="del" data-name="' + esc(n) + '"><svg class="icon icon-sm"><use href="#i-trash"/></svg>删除</button>' +
        '</div></div>';
    }).join('');
  }

  /* ---------- 操作 ---------- */
  $('cfgList').addEventListener('click', function (e) {
    var btn = e.target.closest('.cfg-op');
    if (!btn) return;
    var act = btn.dataset.act, name = btn.dataset.name;
    if (act === 'connect' && name) doConnect(name);
    if (act === 'disconnect') doDisconnect();
    if (act === 'edit' && name) doEdit(name);
    if (act === 'del' && name) doDelete(name);
  });

  $('connectBtn').addEventListener('click', function () {
    if (!state.configs.length) { toast('请先在「配置」页导入 .ovpn 配置'); return; }
    var target = state.status && state.status.connected ? state.status.name : state.configs[0];
    doConnect(target);
  });
  $('disconnectBtn').addEventListener('click', doDisconnect);

  // 自动重连次数输入框仅开关开启时显示
  function syncReconnectField() {
    var f = $('reconnectMaxField');
    if (f) f.style.display = $('autoReconnectChk').checked ? '' : 'none';
  }
  // 设置统一保存：开机自启 + 自动重连 + 最大次数一次提交
  function saveAutoSettings() {
    var max = parseInt($('reconnectMax').value, 10);
    if (isNaN(max) || max < 0) max = 0;
    api.post('/auto', {
      enable: $('autoChk').checked,
      auto_reconnect: $('autoReconnectChk').checked,
      reconnect_max: max
    }).then(function (d) {
      if (d.error) { toast(d.error); refresh(); }
    });
  }
  $('autoChk').addEventListener('change', saveAutoSettings);
  $('autoReconnectChk').addEventListener('change', function (e) {
    syncReconnectField();
    saveAutoSettings();
  });
  $('reconnectMax').addEventListener('change', saveAutoSettings);
  $('reconnectMax').addEventListener('input', function () {
    // 输入时不保存，仅失焦/回车（change）时保存；防止拖动数字时频繁请求
    var v = parseInt(this.value, 10);
    if (!isNaN(v) && v >= 0) this.value = v;
  });

  // 错误信息友好化映射
  function friendlyError(msg) {
    if (!msg) return '连接失败';
    var m = msg.replace(/^连接失败:\s*/, '');
    // 常见错误映射
    var map = [
      [/TCP: connect.*failed.*Connection refused/i, '服务器拒绝连接（服务端未启动或端口未开放）'],
      [/TCP: connect.*failed.*No route to host/i, '无法到达服务器（网络不通或防火墙拦截）'],
      [/TCP: connect.*failed.*Network is unreachable/i, '网络不可达（请检查网络连接）'],
      [/TCP: connect.*failed.*Invalid argument/i, '服务器地址格式错误（请检查 remote 地址）'],
      [/AUTH_FAILED/i, '认证失败（用户名或密码错误）'],
      [/TLS Error/i, 'TLS 握手失败（证书或密钥问题）'],
      [/Resolv.*failed|Name or service not known/i, 'DNS 解析失败（服务器地址无法解析）'],
      [/Could not determine IPv4\/IPv6 protocol/i, '协议选择失败（请检查服务器配置）'],
      [/连接未建立/i, '连接超时（服务器无响应）'],
    ];
    for (var i = 0; i < map.length; i++) {
      if (map[i][0].test(m)) return map[i][1];
    }
    return m.length > 60 ? m.substring(0, 57) + '...' : m;
  }

  // 连接失败原因弹窗（v0.1.9）：展示具体失败原因 + 相关日志，常驻直到用户关闭
  function showFailDialog(headline, raw, failLog) {
    var d = $('failDialog');
    if (!d) return;
    $('failHeadline').textContent = headline || '连接失败';
    $('failReason').textContent = raw || '(无具体信息)';
    $('failLog').textContent = failLog || '(无可显示日志)';
    if (!d.open) d.showModal();
  }
  function closeFailDialog() { var d = $('failDialog'); if (d && d.open) d.close(); }
  $('failClose').addEventListener('click', closeFailDialog);
  $('failOk').addEventListener('click', closeFailDialog);
  $('failViewLog').addEventListener('click', function () {
    closeFailDialog();
    showPanel('logs');   // 切到日志页并刷新，便于进一步排查
  });

  function doConnect(name) {
    var btn = $('connectBtn');
    if (btn) { btn.disabled = true; btn.textContent = '连接中...'; }
    toast('正在连接 ' + name + ' ...');
    startFastPoll();   // 立即 2 秒快轮询，状态实时刷新（不依赖 fetch 返回）
    api.post('/connect', { name: name }).then(function (d) {
      if (btn) { btn.disabled = false; btn.innerHTML = '<svg class="icon icon-sm"><use href="#i-bolt"/></svg>连接'; }
      if (d.error) {
        // 弹窗展示具体失败原因（raw = 去掉“连接失败: ”前缀后的真实 OpenVPN 错误行）
        var raw = String(d.error).replace(/^连接失败:\s*/, '');
        showFailDialog(friendlyError(d.error), raw, d.fail_log);
      } else { toast('连接成功'); }
      refresh();
    });
  }
  function doDisconnect() {
    api.post('/disconnect').then(function (d) {
      if (d.error) { toast(d.error); return; }
      refresh();
    });
  }
  function doEdit(name) {
    api.get('/configs/' + encodeURIComponent(name)).then(function (d) {
      if (d.error) { toast(d.error); return; }
      editOrigin = d.name || name;
      $('inpName').value = editOrigin;
      $('inpContent').value = d.content || '';
      $('inpUser').value = d.username || '';
      $('inpPass').value = '';
      $('cfgDialogTitle').textContent = '编辑配置 · ' + editOrigin;
      $('importErr').textContent = '';
      $('cfgDialog').showModal();
      $('inpName').focus();
    });
  }
  function doDelete(name) {
    if (!confirm('删除配置「' + name + '」？')) return;
    api.del('/configs/' + encodeURIComponent(name)).then(function (d) {
      if (d.error) { toast(d.error); return; }
      refresh();
    });
  }
  /* ---------- .ovpn 文件导入（选择 / 拖拽） ---------- */
  $('pickFileBtn').addEventListener('click', function () {
    // 打开系统文件框；关闭后会补发一次游离 click（坐标落在弹窗外）→ 屏蔽一小段时间，避免误关弹窗
    suppressDialogClickUntil = Date.now() + 1500;
    $('inpFile').click();
  });
  $('inpFile').addEventListener('change', function () {
    var f = this.files && this.files[0];
    if (!f) return;
    readOvpnFile(f);
    this.value = '';
  });
  var dropZone = $('inpContent');
  ['dragover', 'dragenter'].forEach(function (ev) {
    dropZone.addEventListener(ev, function (e) { e.preventDefault(); dropZone.style.borderColor = 'var(--primary)'; });
  });
  ['dragleave', 'drop'].forEach(function (ev) {
    dropZone.addEventListener(ev, function (e) { e.preventDefault(); dropZone.style.borderColor = ''; });
  });
  dropZone.addEventListener('drop', function (e) {
    var f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
    if (f) readOvpnFile(f);
  });
  function readOvpnFile(f) {
    var reader = new FileReader();
    reader.onload = function () {
      $('inpContent').value = String(reader.result || '');
      // 名称自动取文件名（去扩展名），不覆盖用户已输入
      var n = (f.name || '').replace(/\.(ovpn|conf|txt)$/i, '').trim();
      if (n && !$('inpName').value.trim()) $('inpName').value = n;
      $('fileHint').textContent = '已载入：' + f.name + '（' + (f.size / 1024).toFixed(1) + ' KB）';
    };
    reader.readAsText(f);
  }

  function doImport() {
    var name = $('inpName').value.trim(), content = $('inpContent').value.trim();
    $('importErr').textContent = '';
    if (!name || !content) { $('importErr').textContent = '请填写名称和配置内容'; return; }
    api.post('/import', { name: name, content: content, username: $('inpUser').value.trim(), password: $('inpPass').value }).then(function (d) {
      if (d.error) { $('importErr').textContent = d.error; return; }
      // 编辑时改了名：保存新名后删掉旧配置，避免重复
      if (editOrigin && editOrigin !== name) { api.del('/configs/' + encodeURIComponent(editOrigin)); }
      editOrigin = '';
      $('inpName').value = ''; $('inpContent').value = ''; $('inpUser').value = ''; $('inpPass').value = '';
      closeDialog();
      toast('配置已保存');
      refresh();
    });
  }

  /* ---------- 关于：检测更新 + Bug 反馈 ---------- */
  function pickVer(s) { var m = String(s || '').match(/(\d+\.\d+\.\d+)/); return m ? m[1] : ''; }
  function cmpVer(a, b) {
    var pa = String(a).split('.').map(Number), pb = String(b).split('.').map(Number);
    for (var i = 0; i < 3; i++) { var x = pa[i] || 0, y = pb[i] || 0; if (x !== y) return x - y; }
    return 0;
  }
  $('checkUpdateBtn').addEventListener('click', function () { checkUpdate(false); });
  function checkUpdate(silent) {
    var st = $('updateStatus'), res = $('updateResult');
    if (st) st.textContent = '检查中...';
    api.get('/about/update').then(function (d) {
      if (!d || d.ok === false) { if (st) st.textContent = (d && d.error) || '检查失败'; return; }
      var rels = (d.releases || []);
      var latest = rels[0] || null, lat = pickVer(latest && latest.version);
      var cur = pickVer(d.current);
      var hasNew = !!(cur && lat && cmpVer(lat, cur) > 0);
      if (st) st.textContent = lat ? (hasNew ? '发现新版本 v' + lat + '！' : '已是最新版本') : '暂无版本信息';
      if (res) {
        res.hidden = false;
        if (!latest) { res.innerHTML = '暂无发布版本。'; }
        else if (hasNew) {
          var dl = latest.download_url ? ' <a href="' + esc(latest.download_url) + '" target="_blank" rel="noopener" style="color:var(--primary);font-weight:600">下载</a>' : '';
          res.innerHTML = '<div style="padding:8px 10px;background:var(--primary-soft);border-radius:8px"><b>发现新版本 v' + esc(lat) + '</b>（' + esc((latest.pub_date || '').slice(0, 10)) + ' 发布）—— 请到飞牛「应用中心」手动升级安装。' + dl + '</div>';
        } else {
          res.innerHTML = '<div class="hint" style="margin:0">当前已是最新版本 v' + esc(cur) + '（' + esc((latest.pub_date || '').slice(0, 10)) + ' 发布）。</div>';
        }
        var chg = $('changelogList');
        if (chg) {
          var rels2 = rels.filter(function (r) { return r.version; });
          chg.innerHTML = rels2.length ? rels2.map(function (r) {
            var body = esc(r.content || r.title || '（无说明）').replace(/\n/g, '<br>');
            return '<div style="padding:8px 0;border-bottom:1px solid var(--border)"><b>v' + esc(r.version) + '</b> <span class="hint">' + esc((r.pub_date || '').slice(0, 10)) + '</span><div style="margin-top:4px;font-size:13px;line-height:1.6">' + body + '</div></div>';
          }).join('') : '暂无历史版本。';
        }
      }
    });
  }

  $('fbSubmit').addEventListener('click', function () {
    var title = $('fbTitle').value.trim(), err = $('fbErr');
    err.textContent = '';
    if (!title) { err.textContent = '请填写标题'; return; }
    var btn = $('fbSubmit'); btn.disabled = true; btn.textContent = '提交中...';
    api.post('/about/feedback', {
      category: $('fbCategory').value, title: title,
      description: $('fbDesc').value.trim(), contact: $('fbContact').value.trim(),
      include_logs: $('fbLogs').checked
    }).then(function (d) {
      btn.disabled = false; btn.innerHTML = '<svg class="icon icon-sm"><use href="#i-send"/></svg>提交反馈';
      if (d && d.ok) {
        err.style.color = 'var(--success)';
        err.textContent = '反馈已提交，感谢！';
        $('fbTitle').value = ''; $('fbDesc').value = ''; $('fbContact').value = '';
        setTimeout(function () { err.textContent = ''; }, 4000);
      } else {
        err.style.color = '';
        err.textContent = (d && d.error) || '提交失败';
      }
    });
  });

  /* ---------- 日志 ---------- */
  function refreshLog() {
    api.get('/log?lines=200').then(function (d) {
      $('logBox').textContent = d.log || '(空)';
    });
  }

  /* ---------- 启动 ---------- */
  document.body.classList.remove('boot');
  refresh();
  refreshLog();
  setInterval(refresh, 8000);
  setInterval(refreshLog, 15000);
})();
