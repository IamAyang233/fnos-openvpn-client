import paramiko, shlex
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo %s | sudo -S bash -c %s" % (shlex.quote(PASS), shlex.quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + " |E:" + stderr.read().decode().strip()
print("APP_CENTER:")
print(run("ls -la /vol2/@appcenter/openvpn-client/ 2>/dev/null | head -20 || echo NO_APP_CENTER_DIR"))
print("APP_CENTER cmd:")
print(run("ls -la /vol2/@appcenter/openvpn-client/cmd/ 2>/dev/null | head -20 || echo NO_CMD"))
print("VAR_APPS symlink:")
print(run("ls -la /var/apps/openvpn-client 2>/dev/null || echo NO_VAR_SYMLINK"))
c.close()
