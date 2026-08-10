import paramiko, shlex
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo %s | sudo -S bash -c %s" % (shlex.quote(PASS), shlex.quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + " |E:" + stderr.read().decode().strip()
print("== app 是否在 app-center 列表 ==")
print(run("curl -s --max-time 5 http://127.0.0.1:18081/api/bootstrap >/dev/null 2>&1; ls /var/apps/ 2>/dev/null"))
print("== @appcenter openvpn-client 残留 ==")
print(run("ls -la /vol2/@appcenter/openvpn-client/ 2>/dev/null | head || echo NO_APP_CENTER"))
print("== /var/apps/openvpn-client 残留 ==")
print(run("ls -la /var/apps/openvpn-client/ 2>/dev/null || echo NO_VAR_APPS"))
print("== wizard ==")
print(run("ls -la /var/apps/openvpn-client/wizard 2>/dev/null || echo WIZARD_GONE", sudo=True))
c.close()
