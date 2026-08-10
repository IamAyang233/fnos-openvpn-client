import paramiko
c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect("192.168.100.254", username="ja233", password="Pbj781230.", timeout=10)
def run(cmd):
    stdin, stdout, stderr = c.exec_command(cmd)
    return (stdout.read().decode().strip() + stderr.read().decode().strip())
print("ARCH:", run("uname -m"))
print("KERNEL:", run("uname -s"))
print("WIZARD:", run("ls -la /var/apps/openvpn-client/wizard 2>/dev/null || echo NO_WIZARD_DIR"))
print("APP DIR:", run("ls -la /var/apps/openvpn-client/ 2>/dev/null || echo NO_APP_DIR"))
c.close()
