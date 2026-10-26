package app

import "runtime"

var goos = runtime.GOOS

// messages는 결과 코드별 한국어 안내다. 원문 오류 문자열(경로·URL 포함 가능)은 쓰지 않는다.
var messages = map[string]string{
	"core_missing":                     "Collector Core(agent-office-collector)를 찾지 못했습니다. GUI와 같은 폴더에 두거나 AGENT_OFFICE_COLLECTOR_BIN에 절대 경로를 지정하세요.",
	"core_timeout":                     "Collector Core가 제한 시간 안에 응답하지 않았습니다.",
	"core_start_failed":                "Collector Core를 실행하지 못했습니다(실행 권한 확인).",
	"core_output_invalid":              "Collector Core 출력 형식이 예상과 다릅니다. GUI와 Core 버전을 맞추세요.",
	"invalid_server_url":               "서버 주소 형식이 올바르지 않습니다(예: https://office.example, 경로 없이).",
	"insecure_server_url":              "서버 주소는 https여야 합니다(http는 localhost·127.x·::1만 허용).",
	"invalid_registration_code":        "등록 코드 형식이 올바르지 않습니다(XXXX-XXXX 8자).",
	"invalid_device_name":              "PC 이름은 1~80자여야 하고 제어 문자를 넣을 수 없습니다.",
	"prefs_unavailable":                "GUI 설정 위치를 찾을 수 없습니다.",
	"prefs_write_failed":               "GUI 설정을 저장하지 못했습니다.",
	"server_saved":                     "서버 주소를 저장했습니다.",
	"server_not_set":                   "먼저 서버 주소를 저장하세요.",
	"browser_unavailable":              "브라우저를 열 수 없습니다. 주소를 직접 여세요.",
	"browser_opened":                   "브라우저에서 로그인한 뒤 'PC 등록 코드 발급'으로 코드를 받아 아래에 입력하세요.",
	"register_failed":                  "등록하지 못했습니다.",
	"already_registered":               "이미 등록된 PC입니다. 새 장치로 다시 등록하려면 '다시 등록'을 선택하세요.",
	"registered":                       "등록했습니다. 장치 토큰은 Collector Core가 OS 보안 저장소(없으면 권한 0600 설정 파일)에 저장하며 GUI에는 표시하지 않습니다.",
	"paused":                           "수집을 일시중지했습니다. 실행 중인 Collector가 몇 초 안에 반영합니다.",
	"resumed":                          "수집을 재개했습니다. 일시중지 동안의 기록은 수집하지 않습니다.",
	"pause_failed":                     "일시중지하지 못했습니다.",
	"resume_failed":                    "재개하지 못했습니다.",
	"hook_preview":                     "아래 변경 미리보기를 확인하세요. 아직 파일을 바꾸지 않았습니다.",
	"hook_preview_failed":              "Hook 미리보기를 만들지 못했습니다.",
	"confirmation_required":            "변경하려면 명시적으로 승인해야 합니다.",
	"preview_required":                 "먼저 설치 미리보기를 확인하세요.",
	"hook_installed":                   "Claude Code Hook을 설치했습니다(원본 백업 후 Agent Office 항목만 추가). 새로 여는 Claude 세션부터 적용됩니다.",
	"hook_install_failed":              "Hook을 설치하지 못했습니다. 설정 파일은 바뀌지 않았거나 백업이 남아 있습니다.",
	"hook_uninstalled":                 "Agent Office Hook만 제거했습니다.",
	"hook_uninstall_failed":            "Hook을 제거하지 못했습니다.",
	"codex_hook_installed":             "Codex Hook을 설치했습니다(원본 백업 후 Agent Office 항목만 추가). Codex를 실행해 \"Hooks need review\"에서 신뢰해야 동작합니다.",
	"invalid_tool":                     "지원하지 않는 도구입니다.",
	"service_preview":                  "자동 시작 정의 미리보기입니다. 아직 파일을 쓰지 않았습니다.",
	"service_preview_failed":           "자동 시작 정의를 만들지 못했습니다.",
	"service_installed":                "자동 시작 정의를 설치했습니다. GUI를 닫아도 수집은 계속됩니다.",
	"service_install_failed":           "자동 시작 정의를 설치하지 못했습니다(소유하지 않은 같은 이름 파일은 덮어쓰지 않습니다).",
	"logged_out":                       "로그아웃했습니다. 서버에서 이 PC를 폐기했고 로컬 장치 토큰을 지웠습니다. 실행 중인 Collector는 몇 초 안에 전송을 멈춥니다.",
	"logged_out_local":                 "로컬 장치 토큰만 지웠습니다. 웹 /devices에서 이 PC를 폐기하세요.",
	"logout_failed":                    "로그아웃하지 못했습니다. 로컬 정보는 그대로입니다(서버 연결 실패 시 '로컬만 지우기'를 선택할 수 있습니다).",
	"gui_autostart_installed":          "로그인 시 트레이를 자동으로 띄웁니다(창은 트레이의 '설정'으로 엽니다). 수집은 Core 자동 시작과 별개로 계속됩니다.",
	"gui_autostart_removed":            "GUI 자동 시작 정의를 지웠습니다(Agent Office 소유 항목만). Core 자동 시작은 그대로입니다.",
	"gui_autostart_absent":             "설치된 GUI 자동 시작 정의가 없습니다.",
	"gui_autostart_foreign":            "같은 위치에 Agent Office가 만들지 않은 항목이 있어 건드리지 않았습니다.",
	"gui_autostart_invalid_exe":        "GUI 실행 파일 위치가 임시 경로이거나 올바르지 않아 자동 시작을 설치할 수 없습니다. 고정 위치에 둔 GUI로 실행하세요.",
	"gui_autostart_unsupported":        "이 OS에서는 GUI 자동 시작을 지원하지 않습니다.",
	"gui_autostart_failed":             "GUI 자동 시작 정의를 처리하지 못했습니다.",
	"update_latest":                    "최신 버전입니다.",
	"update_available":                 "새 버전이 있습니다. 트레이 메뉴의 '업데이트 설치'를 누르면 받기·서명 확인·설치·후처리까지 합니다.",
	"update_downloaded":                "새 버전 설치 파일을 받아 서명·해시를 확인해 두었습니다. 트레이 메뉴의 '업데이트 설치'로 설치합니다.",
	"update_installer_started":         "업데이트 설치를 시작했습니다. 트레이가 잠시 닫혔다가 설치가 끝나면 다시 뜹니다.",
	"update_not_installer_install":     "설치 프로그램으로 설치한 Collector가 아닙니다. 명령행 설치는 agent-office-collector update apply를 쓰세요.",
	"update_no_installer_for_platform": "이 OS·아키텍처용 설치 파일이 업데이트 정보에 없습니다.",
	"update_confirmation_required":     "업데이트 설치는 승인한 뒤에만 시작합니다.",
	"update_tray_missing":              "트레이 도우미를 찾지 못해 업데이트를 시작하지 않았습니다(설치 프로그램 배치에서만 동작).",
	"update_insecure_url":              "업데이트 주소가 https가 아니어서 거부했습니다.",
	"update_bad_signature":             "업데이트 정보의 서명이 맞지 않아 거부했습니다(변조 또는 키 불일치).",
	"update_no_trusted_key":            "이 빌드에는 업데이트 서명 키가 없어 업데이트를 확인하지 않습니다.",
	"update_checksum_mismatch":         "받은 설치 파일의 해시가 맞지 않아 거부했습니다.",
	"update_prefs_saved":               "업데이트 설정을 저장했습니다.",
	"invalid_update_prefs":             "업데이트 설정 값이 올바르지 않습니다.",
	"setup_saved":                      "설정 마법사 상태를 저장했습니다.",
	"update_check_failed":              "업데이트를 확인하지 못했습니다(네트워크 또는 업데이트 서명 키 확인).",
	"unexpected":                       "요청을 처리하지 못했습니다.",
}

func messageFor(code string) string {
	if m, ok := messages[code]; ok {
		return m
	}
	return messages["unexpected"]
}
