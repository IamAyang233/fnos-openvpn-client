import paramiko, time
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd):
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + stderr.read().decode().strip()
time.sleep(8)
print("== 进程 ==")
print(run("pgrep -af openvpn-client-web | head -3 || echo NO_WEB_PROC"))
print("== 版本指纹（二进制）==")
print(run("strings /var/apps/openvpn-client/target/bin/openvpn-client-web 2>/dev/null | grep -o '0\.1\.7' | head -1 || echo NO_BIN"))
print("== settings/status 文件 ==")
print(run("ls -la /vol2/@appdata/openvpn-client/etc/ 2>/dev/null || echo NO_DATA"))
print("== API 探测 (version) ==")
print(run("curl -s --max-time 5 -H 'X-Client-Token: fnos-openvpn-client' http://127.0.0.1:18081/api/bootstrap 2>/dev/null | head -c 400 || echo CURL_FAIL"))
c.close()
