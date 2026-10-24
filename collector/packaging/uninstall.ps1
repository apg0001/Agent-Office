# 사용자별 Collector 정리(#42, Windows). 기본은 Hook·로그온 작업만 지우고 설정·토큰·큐는 남긴다.
#   powershell -ExecutionPolicy Bypass -File uninstall.ps1 [-Purge] [-Yes]
param([switch]$Purge, [switch]$Yes)
$ErrorActionPreference = "Stop"
$bin = Join-Path $PSScriptRoot "agent-office-collector.exe"
if (-not $Yes) {
  $answer = Read-Host "Agent Office Hook·자동 시작을 지웁니다$(if ($Purge) { '(설정·토큰·큐 포함)' }). 계속할까요? [y/N]"
  if ($answer -notin @("y", "Y")) { Write-Output "취소"; exit 1 }
}
& $bin hooks uninstall --tool claude --yes 2>$null | Out-Null
& $bin service uninstall 2>$null | Out-Null
if ($Purge) {
  # Core의 경로 규칙과 같다(%APPDATA%·%LOCALAPPDATA%\AgentOffice\Collector).
  foreach ($dir in @((Join-Path $env:APPDATA "AgentOffice\Collector"), (Join-Path $env:LOCALAPPDATA "AgentOffice\Collector"))) {
    if ($dir.EndsWith("AgentOffice\Collector") -and (Test-Path $dir)) { Remove-Item -Recurse -Force -LiteralPath $dir }
  }
}
Write-Output "정리 완료"
