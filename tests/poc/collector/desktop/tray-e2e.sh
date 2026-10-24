set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends libgtk-3-0 libwebkit2gtk-4.0-37 xvfb xdotool dbus dbus-x11 python3-dbus python3-gi gir1.2-glib-2.0 procps curl >/dev/null 2>&1
dpkg-query -W -f='${Package} ${Version}\n' libwebkit2gtk-4.0-37 libgtk-3-0 xvfb dbus python3-dbus
mkdir -p /w/bin /w/home && cp /bin-in/agent-office-collector /bin-in/agent-office-collector-gui /w/bin/
cat > /w/inner.sh <<'EOS'
set -e
export HOME=/w/home AGENT_OFFICE_COLLECTOR_HOME=/w/ao AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file DISPLAY=:99
C=/w/bin/agent-office-collector
Xvfb :99 -screen 0 1280x900x24 >/dev/null 2>&1 &
python3 /scripts/sni_watcher.py /w/items &
python3 /scripts/fake_office.py 18080 &
sleep 2
$C register --server http://127.0.0.1:18080 --code ABCD-EFGH --name TrayPC | head -1
$C run 2>/w/core.log &
CORE=$!
sleep 2
$C status --json | grep -E '"(registration|token_storage|process_running)"'
$C-gui --autostart >/w/gui.log 2>&1 &
GUI=$!
sleep 8
echo "== watcher registrations"; cat /w/items
SENDER=$(awk '{print $1}' /w/items | head -1)
echo "== visible windows after --autostart (expect none)"; xdotool search --onlyvisible --name "Agent Office Collector" || echo "(none)"
echo "== tooltip/icon"; python3 /scripts/sni_client.py "$SENDER" tooltip
echo "== menu"; python3 /scripts/sni_client.py "$SENDER" layout
echo "== click pause"; python3 /scripts/sni_client.py "$SENDER" click "수집 일시중지"; sleep 2
$C status --json | grep '"paused"'
python3 /scripts/sni_client.py "$SENDER" layout | sed -n '1p;4,5p'
python3 /scripts/sni_client.py "$SENDER" tooltip | head -1
echo "== click resume"; python3 /scripts/sni_client.py "$SENDER" click "수집 재개"; sleep 2
$C status --json | grep '"paused"'
python3 /scripts/sni_client.py "$SENDER" layout | head -1
echo "== click open window"; python3 /scripts/sni_client.py "$SENDER" click "창 열기"; sleep 2
xdotool search --onlyvisible --name "Agent Office Collector" >/dev/null && echo "window visible" || echo "window NOT visible"
echo "== second instance"
set +e
timeout 15 $C-gui >/w/gui2.log 2>&1; echo "second instance exit $?"
set -e
kill -0 $GUI && echo "first GUI still running"
echo "== core killed while GUI runs"
kill -9 $CORE; sleep 7
python3 /scripts/sni_client.py "$SENDER" layout | head -1
$C run 2>>/w/core.log &
CORE=$!
sleep 7
echo "== core restarted"; python3 /scripts/sni_client.py "$SENDER" layout | head -1
echo "== click quit GUI"; python3 /scripts/sni_client.py "$SENDER" click "GUI 종료"; sleep 3
if kill -0 $GUI 2>/dev/null; then echo "GUI still running (unexpected)"; else echo "GUI exited"; fi
kill -0 $CORE && echo "core still running after GUI quit"
$C status --json | grep '"process_running"'
B=$(curl -s http://127.0.0.1:18080/counts); echo "server before hook: $B"
echo '{"hook_event_name":"SessionStart","session_id":"00000000-0000-4000-8000-000000000301","source":"startup"}' | $C hook claude
sleep 5
echo "server after hook (GUI gone): $(curl -s http://127.0.0.1:18080/counts)"
echo "== gui log"; grep -v '^$' /w/gui.log | head -20 || true
grep -c "aod_SENTINEL" /w/gui.log /w/core.log || true
kill $CORE || true
EOS
dbus-run-session -- sh /w/inner.sh
