import paramiko, shlex
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo %s | sudo -S bash -c %s" % (shlex.quote(PASS), shlex.quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + " |E:" + stderr.read().decode().strip()
print("== 当前服务 ==")
print(run("pgrep -af openvpn-client-web | head -1 || echo NO_WEB"))
print("== 当前 manifest ==")
print(run("cat /vol2/@appcenter/openvpn-client/manifest 2>/dev/null | grep -E '^version' || echo NO_MANIFEST", sudo=True))
print("== 修改 manifest version 0.1.6 -> 0.1.7 ==")
print(run("sed -i 's/^version         = 0.1.6$/version         = 0.1.7/' /vol2/@appcenter/openvpn-client/manifest && cat /vol2/@appcenter/openvpn-client/manifest | grep -E '^version'", sudo=True))
print("== 修改后 API 版本 ==")
print(run("curl -s --max-time 5 -H 'X-Client-Token: fnos-openvpn-client' http://127.0.0.1:18081/api/bootstrap 2>/dev/null | grep -o '\"version\":\"[0-9.]*\"' || echo CURL_FAIL"))
c.close()
