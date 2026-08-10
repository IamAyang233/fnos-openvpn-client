import paramiko, shlex
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo %s | sudo -S bash -c %s" % (shlex.quote(PASS), shlex.quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + " |E:" + stderr.read().decode().strip()
print("== 找 manifest 位置 ==")
print(run("ls -la /var/apps/openvpn-client/manifest; readlink -f /var/apps/openvpn-client/manifest; ls -la /var/apps/openvpn-client/ | grep -E 'manifest|target'", sudo=True))
print("== 修改 manifest ==")
print(run("sed -i 's/^version         = 0.1.6$/version         = 0.1.7/' /var/apps/openvpn-client/manifest && grep -E '^version' /var/apps/openvpn-client/manifest", sudo=True))
c.close()
