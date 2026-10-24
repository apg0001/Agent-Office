set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends gnome-keyring dbus dbus-x11 >/dev/null
dpkg-query -W -f='${Package} ${Version}\n' gnome-keyring dbus
mkdir -p /w/r && cp -r /src/. /w/r/ && cd /w/r/collector/core
go test -c -o /w/cli.test ./internal/cli
go test -c -o /w/secret.test ./internal/secret
mkdir -p /root/.cache /run/user/0
cat > /w/inner.sh <<'EOS'
set -e
printf 'poc-keyring-pass' | gnome-keyring-daemon --unlock --components=secrets >/dev/null
sleep 1
cd /w/r/collector/core/internal/secret && AGENT_OFFICE_TEST_REAL_KEYRING=1 /w/secret.test -test.run 'TestRealKeyring' -test.v
cd /w/r/collector/core/internal/cli && AGENT_OFFICE_TEST_REAL_KEYRING=1 /w/cli.test -test.run 'TestRealKeyring' -test.v
EOS
dbus-run-session -- sh /w/inner.sh
