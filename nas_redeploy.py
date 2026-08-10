import paramiko, subprocess, shlex, time
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
TRIM = r"C:/Users/aykej/.workbuddy/skills/trim-cli/bin/trim-cli-windows-x64.exe"
FPK  = r"D:/AI项目/2026-07-21-00-18-48/apps/openvpn-client-fpk/openvpn-client_0.1.7_x86.fpk3"

c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd, sudo=False):
    if sudo:
        cmd = "echo %s | sudo -S bash -c %s" % (shlex.quote(PASS), shlex.quote(cmd))
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + " |ERR:" + stderr.read().decode().strip()

print("== [1] 删 wizard ==")
print(run("rm -rf /var/apps/openvpn-client/wizard", sudo=True))
print("verify:", run("ls -la /var/apps/openvpn-client/wizard 2>/dev/null || echo WIZARD_GONE", sudo=True))
print("== [2] uninstall ==")
c.close()
r = subprocess.run([TRIM,"--host",HOST,"--scheme","ws","--allow-insecure-ws","app","uninstall","openvpn-client","--yes"],capture_output=True,text=True,timeout=120)
print("uninstall rc:", r.returncode, "out:", r.stdout[:400], "err:", r.stderr[:300])
print("== [3] install fpk3 ==")
r2 = subprocess.run([TRIM,"--host",HOST,"--scheme","ws","--allow-insecure-ws","app","install-fpk","--volume-id","2",FPK,"--yes"],capture_output=True,text=True,timeout=240)
print("install rc:", r2.returncode, "out:", r2.stdout[:400], "err:", r2.stderr[:300])
