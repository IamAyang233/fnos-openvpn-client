import paramiko, time
HOST, USER, PASS = "192.168.100.254", "ja233", "Pbj781230."
c = paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(HOST, username=USER, password=PASS, timeout=10)
def run(cmd):
    stdin, stdout, stderr = c.exec_command(cmd)
    return stdout.read().decode().strip() + stderr.read().decode().strip()
time.sleep(15)
print("PROC:", run("pgrep -af openvpn-client-web | head -1 || echo NO_WEB"))
print("API:", run("curl -s --max-time 5 -H 'X-Client-Token: fnos-openvpn-client' http://127.0.0.1:18081/api/bootstrap 2>/dev/null | head -c 200 || echo CURL_FAIL"))
c.close()
