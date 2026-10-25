# 사용자별 Collector 정리(#42, Windows). 기본은 Hook·로그온 작업만 지우고 설정·토큰·큐는 남긴다.
#   powershell -ExecutionPolicy Bypass -File uninstall.ps1 [-Purge] [-Yes]
# 파일은 UTF-8 BOM으로 저장한다. BOM이 없으면 Windows PowerShell 5.1이 ANSI로 읽어 한글이 깨지고 구문 오류가 난다.
param([switch]$Purge, [switch]$Yes)
# 네이티브 명령의 stderr를 종료 오류로 바꾸지 않도록 Stop은 PowerShell cmdlet에만 개별로 건다.
$ErrorActionPreference = "Continue"
$bin = Join-Path $PSScriptRoot "agent-office-collector.exe"
if (-not $Yes) {
  $answer = Read-Host "Agent Office Hook·자동 시작을 지웁니다$(if ($Purge) { '(설정·토큰·큐 포함)' }). 계속할까요? [y/N]"
  if ($answer -notin @("y", "Y")) { Write-Output "취소"; exit 1 }
}
& $bin hooks uninstall --tool claude --yes *> $null
& $bin service uninstall *> $null
# GUI(트레이) 자동 시작(#43): 우리 형식("...\agent-office-collector-gui.exe" --autostart)인 Run 값만 지운다.
$run = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$gui = (Get-ItemProperty -Path $run -Name AgentOfficeCollectorGUI -ErrorAction SilentlyContinue).AgentOfficeCollectorGUI
if ($gui -and $gui -match '^"[A-Za-z]:\\[^"]*\\agent-office-collector-gui\.exe" --autostart$') {
  Remove-ItemProperty -Path $run -Name AgentOfficeCollectorGUI -ErrorAction Stop
}
if ($Purge) {
  # Credential Manager의 장치 토큰 항목(#41)을 먼저 지운다. 서버 폐기는 하지 않는다(웹 /devices 또는 logout).
  & $bin logout --local-only *> $null
  # Core의 경로 규칙과 같다(%APPDATA%·%LOCALAPPDATA%\AgentOffice\Collector).
  foreach ($dir in @((Join-Path $env:APPDATA "AgentOffice\Collector"), (Join-Path $env:LOCALAPPDATA "AgentOffice\Collector"))) {
    if ($dir.EndsWith("AgentOffice\Collector") -and (Test-Path $dir)) { Remove-Item -Recurse -Force -LiteralPath $dir -ErrorAction Stop }
  }
}
Write-Output "정리 완료"
exit 0
