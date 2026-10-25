set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq --no-install-recommends libgtk-3-0 libwebkit2gtk-4.0-37 xvfb xdotool wmctrl openbox dbus dbus-x11 python3-dbus python3-gi gir1.2-glib-2.0 procps >/dev/null 2>&1
dpkg-query -W -f='${Package} ${Version}\n' openbox wmctrl
mkdir -p /w/bin /w/home && cp /bin-in/agent-office-collector /bin-in/agent-office-collector-gui /w/bin/
cat > /w/inner.sh <<'EOS'
set -e
export HOME=/w/home AGENT_OFFICE_COLLECTOR_HOME=/w/ao AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file DISPLAY=:99
C=/w/bin/agent-office-collector
Xvfb :99 -screen 0 1280x900x24 >/dev/null 2>&1 &
sleep 1
openbox >/dev/null 2>&1 &
sleep 1
echo "### case A: no StatusNotifierWatcher (e.g. stock GNOME) -> close must quit GUI"
$C-gui >/w/guiA.log 2>&1 &
G=$!
sleep 7
wmctrl -l | grep -c "Agent Office Collector" || true
wmctrl -c "Agent Office Collector"; sleep 3
if kill -0 $G 2>/dev/null; then echo "A: GUI still running (unexpected)"; kill $G; else echo "A: GUI exited on close"; fi
echo "### case B: watcher present -> close hides to tray"
python3 /scripts/sni_watcher.py /w/items &
sleep 2
$C-gui >/w/guiB.log 2>&1 &
G=$!
sleep 7
wmctrl -c "Agent Office Collector"; sleep 3
if kill -0 $G 2>/dev/null; then echo "B: GUI still running after close"; else echo "B: GUI exited (unexpected)"; fi
xdotool search --onlyvisible --name "Agent Office Collector" >/dev/null && echo "B: window visible (unexpected)" || echo "B: window hidden"
SENDER=$(awk '{print $1}' /w/items | tail -1)
python3 /scripts/sni_client.py "$SENDER" click "창 열기"; sleep 2
xdotool search --onlyvisible --name "Agent Office Collector" >/dev/null && echo "B: window visible again via tray" || echo "B: window NOT visible"
python3 /scripts/sni_client.py "$SENDER" click "GUI 종료"; sleep 3
if kill -0 $G 2>/dev/null; then echo "B: GUI still running (unexpected)"; else echo "B: GUI exited via tray quit"; fi
EOS
dbus-run-session -- sh /w/inner.sh
