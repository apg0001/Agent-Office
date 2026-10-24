#!/bin/sh
# 패키지 제거는 바이너리만 지운다. 사용자별 Hook·서비스·설정은 패키지가 다른 사용자 홈을
# 건드리지 않도록 그대로 둔다. 먼저 각 사용자가 agent-office-collector-uninstall을 실행한다.
echo "사용자별 Hook·자동 시작을 지우려면 제거 전에 각 계정에서 agent-office-collector-uninstall을 실행하세요."
exit 0
