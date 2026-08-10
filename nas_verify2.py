import paramiko, time
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd):
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + stderr.read().decode().strip()
time.sleep(12)
print("== 进程 ==")
print(run("pgrep -af openvpn-client-web | head -2 || echo NO_WEB_PROC"))
print("== bootstrap version ==")
print(run("curl -s --max-time 5 -H 'X-Client-Token: fnos-openvpn-client' http://127.0.0.1:18081/api/bootstrap 2>/dev/null | head -c 300 || echo CURL_FAIL"))
print("== 已装 app 版本(manifest) ==")
print(run("cat /var/apps/openvpn-client/manifest 2>/dev/null | grep version || echo NO_MANIFEST"))
c.close()
