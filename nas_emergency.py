import subprocess
HOST = "192.168.100.254"
TRIM = r"C:/Users/aykej/.workbuddy/skills/trim-cli/bin/trim-cli-windows-x64.exe"
FPK  = r"D:/AI项目/2026-07-21-00-18-48/apps/openvpn-client-fpk/openvpn-client_0.1.7_x86.fpk"
print("== EMERGENCY install (0.1.7 original fpk, manifest 0.1.6) ==")
r = subprocess.run([TRIM,"--host",HOST,"--scheme","ws","--allow-insecure-ws","app","install-fpk","--volume-id","2",FPK,"--yes"],capture_output=True,text=True,timeout=240)
print("rc:", r.returncode, "out:", r.stdout[:500], "err:", r.stderr[:400])
