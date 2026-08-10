import paramiko, subprocess, sys

HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
TRIM = r"C:/Users/aykej/.workbuddy/skills/trim-cli/bin/trim-cli-windows-x64.exe"
FPK  = r"D:/AI项目/2026-07-21-00-18-48/apps/openvpn-client-fpk/openvpn-client_0.1.7_x86.fpk"

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo '%s' | sudo -S bash -c %s" % (PASS, __import__('shlex').quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    out = stdout.read().decode().strip()
    err = stderr.read().decode().strip()
    return out, err

print("== [1] 删除 wizard (sudo) ==")
out, err = run("rm -rf /var/apps/openvpn-client/wizard", sudo=True)
print("rm wizard out:", out, "err:", err)
# 验证
out, err = run("ls -la /var/apps/openvpn-client/wizard 2>/dev/null || echo WIZARD_GONE", sudo=True)
print("verify:", out, err)

print("== [2] trim-cli uninstall openvpn-client ==")
r = subprocess.run([TRIM, "--host", HOST, "--scheme", "ws", "--allow-insecure-ws",
                    "app", "uninstall", "openvpn-client", "--yes"],
                   capture_output=True, text=True, timeout=120)
print("rc:", r.returncode)
print("stdout:", r.stdout[:1500])
print("stderr:", r.stderr[:800])
c.close()
