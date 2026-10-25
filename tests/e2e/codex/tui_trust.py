#!/usr/bin/env python3
"""임시 CODEX_HOME에서 대화형 Codex TUI를 pty로 띄워, 시작 시 "Hooks need review" 화면에서
사용자가 하는 것처럼 "2. Trust all and continue"를 고르고 종료한다(#170).

    python3 -I tui_trust.py <codex_home> <workdir>

Hook 신뢰를 우회하지 않는다. Codex가 자기 UI로 `config.toml`의 `[hooks.state]`를 쓴다.
출력은 고정 문자열(review_prompt=seen|missing)만이다. 화면 내용·경로는 출력하지 않는다.
사용자 실제 ~/.codex에는 쓰지 않도록 codex_home이 HOME/.codex이면 거부한다.
"""

import fcntl
import os
import pty
import re
import select
import struct
import sys
import termios
import time

home, work = os.path.realpath(sys.argv[1]), sys.argv[2]
if home == os.path.realpath(os.path.expanduser("~/.codex")):
    sys.exit("refusing to touch the real ~/.codex")

ANSI = re.compile(
    rb"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-B]|\x1b[=>78]"
)
env = dict(os.environ, CODEX_HOME=home, CODEX_SQLITE_HOME=home, TERM="xterm-256color")
pid, fd = pty.fork()
if pid == 0:
    os.chdir(work)
    # 모델 호출이 일어나지 않도록 닿지 않는 주소를 준다(신뢰 화면만 쓴다).
    os.execvpe("codex", ["codex", "-c", 'openai_base_url="http://127.0.0.1:9/v1"'], env)

fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
buf = b""


def pump(seconds, until=None):
    global buf
    end = time.time() + seconds
    while time.time() < end:
        ready, _, _ = select.select([fd], [], [], 0.2)
        if fd not in ready:
            continue
        try:
            data = os.read(fd, 65536)
        except OSError:
            return False
        if not data:
            return False
        buf += data
        if b"\x1b[6n" in data:  # 커서 위치 질의에 응답
            os.write(fd, b"\x1b[1;1R")
        if until and until in ANSI.sub(b"", buf):
            return True
    return False


seen = pump(20, until=b"are new or changed")
config = os.path.join(home, "config.toml")


def trusted():
    try:
        with open(config, encoding="utf-8") as f:
            return "trusted_hash" in f.read()
    except OSError:
        return False


if seen:
    pump(3)  # 화면이 입력을 받을 준비가 될 때까지
    os.write(fd, b"2")
    pump(3)
    os.write(fd, b"\r")
    for _ in range(20):
        pump(0.5)
        if trusted():
            break
for _ in range(2):
    os.write(fd, b"\x03")
    pump(1)
try:
    os.kill(pid, 9)
except ProcessLookupError:
    pass
ok = seen and trusted()
print(
    "review_prompt="
    + ("seen" if seen else "missing")
    + " trust_written="
    + ("yes" if trusted() else "no")
)
sys.exit(0 if ok else 1)
