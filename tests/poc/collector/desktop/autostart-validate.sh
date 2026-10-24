set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends desktop-file-utils libxml2-utils >/dev/null 2>&1
dpkg-query -W -f='${Package} ${Version}\n' desktop-file-utils libxml2-utils
mkdir -p /w/r /w/dump && cp -r /src/. /w/r/ && cd /w/r/collector/gui
AO_AUTOSTART_DUMP=/w/dump go test -run TestDumpForExternalValidation -v ./internal/autostart | grep -E "^(--- |ok|FAIL)"
ls /w/dump
desktop-file-validate /w/dump/agent-office-collector-gui.desktop && echo "desktop-file-validate: ok"
xmllint --noout /w/dump/io.github.apg0001.agent-office.collector-gui.plist && echo "plist xml well-formed: ok"
grep '^Exec=' /w/dump/agent-office-collector-gui.desktop
