; Agent Office Collector Windows 설치 프로그램(#159). makensis 3.x(Unicode)로 빌드한다.
;   makensis -DVERSION=0.1.0-beta.1 -DVIVERSION=0.1.0.0 -DSTAGE=<바이너리 폴더> -DOUTFILE=<출력.exe> installer.nsi
; 파일은 UTF-8 BOM으로 저장한다(한글 문자열을 makensis가 UTF-8로 읽게).
;
; - 사용자별 설치(%LOCALAPPDATA%\Programs\Agent Office Collector), 관리자 권한을 요구하지 않는다.
; - 서명하지 않는다. 처음 실행 때 SmartScreen 경고가 나올 수 있다(docs/installation/collector-gui.md).
; - 설치는 Hook·Core 자동 시작·트레이 자동 시작을 켜지 않는다. 마침 화면의 "열기"(기본 체크)가 GUI를 띄우고,
;   GUI는 첫 실행 마법사(서버 → 로그인·등록 → 한 화면 동의 → 연결 확인)를 연다(#180).
; - 업데이트(#180): 트레이 승인 뒤 Core 업데이트 도우미가 `/S`로 무인 실행한다. 설치 폴더는 HKCU InstallLocation을
;   그대로 써서 바뀌지 않으므로 Hook 명령 문자열도 그대로다(Codex 재신뢰 불필요). 끝나면 도우미가 트레이를 다시 띄우고
;   트레이가 후처리(update post-install)를 한다.
; - 제거는 uninstall.ps1 -Yes로 이 도구가 만든 Hook·Core 로그온 작업·트레이 Run 값만 지운 뒤 파일을 지운다.
;   다른 사용자·다른 도구의 Claude/Codex 설정은 건드리지 않는다. 설정·장치 토큰·큐는 남긴다(-Purge는 수동).

Unicode true
ManifestDPIAware true
!include "MUI2.nsh"
!include "x64.nsh"

!include "LogicLib.nsh"
!ifndef VERSION
  !error "-DVERSION=<SemVer>가 필요합니다"
!endif
!ifndef VIVERSION
  !error "-DVIVERSION=<X.Y.Z.0>이 필요합니다"
!endif
!ifndef STAGE
  !error "-DSTAGE=<바이너리 폴더>가 필요합니다"
!endif
!ifndef OUTFILE
  !error "-DOUTFILE=<출력 .exe>가 필요합니다"
!endif

!define APPNAME "Agent Office Collector"
!define REGKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\AgentOfficeCollector"
!define TASK "\AgentOffice\Collector"
!define CORE "agent-office-collector.exe"
!define GUI "agent-office-collector-gui.exe"
!define TRAY "agent-office-collector-tray.exe"

Name "${APPNAME}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Agent Office Collector"
InstallDirRegKey HKCU "${REGKEY}" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${VIVERSION}"
VIAddVersionKey /LANG=0 "ProductName" "${APPNAME}"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "FileDescription" "${APPNAME} 설치 프로그램"

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${GUI}"
!define MUI_FINISHPAGE_RUN_TEXT "Agent Office Collector 열기(트레이 아이콘도 함께 뜹니다)"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH
!insertmacro MUI_LANGUAGE "Korean"

; 트레이·GUI 창·Core를 멈춰 설치 폴더 파일 잠금을 푼다(업그레이드·제거). 실패는 무시한다.
!macro StopOurs
  ${If} ${RunningX64}
    ${DisableX64FSRedirection}
  ${EndIf}
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${TRAY}'
  Pop $0
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${GUI}'
  Pop $0
  nsExec::Exec '"$SYSDIR\schtasks.exe" /End /TN "${TASK}"'
  Pop $0
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${CORE}'
  Pop $0
  ${If} ${RunningX64}
    ${EnableX64FSRedirection}
  ${EndIf}
  Sleep 1000
!macroend

Section "Install"
  !insertmacro StopOurs
  SetOutPath "$INSTDIR"
  File "${STAGE}/${CORE}"
  File "${STAGE}/${GUI}"
  File "${STAGE}/${TRAY}"
  File "${STAGE}/uninstall.ps1"
  File "${STAGE}/README.txt"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk" "$INSTDIR\${GUI}" "" "$INSTDIR\${GUI}" 0
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME} 제거.lnk" "$INSTDIR\uninstall.exe"

  WriteRegStr HKCU "${REGKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKCU "${REGKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${REGKEY}" "Publisher" "Agent Office"
  WriteRegStr HKCU "${REGKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${REGKEY}" "DisplayIcon" "$INSTDIR\${GUI}"
  WriteRegStr HKCU "${REGKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${REGKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${REGKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${REGKEY}" "NoRepair" 1

  ; 업그레이드 전에 Core 로그온 작업이 있었다면 새 실행 파일로 다시 시작한다(없으면 실패를 무시).
  ${If} ${RunningX64}
    ${DisableX64FSRedirection}
  ${EndIf}
  nsExec::Exec '"$SYSDIR\schtasks.exe" /Run /TN "${TASK}"'
  Pop $0
  ${If} ${RunningX64}
    ${EnableX64FSRedirection}
  ${EndIf}
SectionEnd

Section "Uninstall"
  ; 사용자별 정리: 이 도구가 만든 Claude Hook, Core 로그온 작업(등록 해제·중지), 트레이 Run 값만 지운다.
  ${If} ${RunningX64}
    ${DisableX64FSRedirection}
  ${EndIf}
  ${If} ${FileExists} "$INSTDIR\uninstall.ps1"
    nsExec::ExecToLog '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\uninstall.ps1" -Yes'
    Pop $0
  ${EndIf}
  ${If} ${RunningX64}
    ${EnableX64FSRedirection}
  ${EndIf}
  !insertmacro StopOurs

  Delete "$INSTDIR\${CORE}"
  Delete "$INSTDIR\${CORE}.bak"
  ; 업데이트 준비 파일(설치 파일·도우미)은 사용자 상태 폴더(%LOCALAPPDATA%\AgentOffice\Collector\updates)에 있다.
  RMDir /r "$LOCALAPPDATA\AgentOffice\Collector\updates"
  Delete "$INSTDIR\${GUI}"
  Delete "$INSTDIR\${TRAY}"
  Delete "$INSTDIR\uninstall.ps1"
  Delete "$INSTDIR\README.txt"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME} 제거.lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"
  DeleteRegKey HKCU "${REGKEY}"
SectionEnd
