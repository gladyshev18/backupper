# Builds the Linux agents (embedded into the panel) and the Windows panel.
# Result: build\bin\Backupper.exe
# Native tools write progress to stderr; rely on exit codes instead of "Stop".
$ErrorActionPreference = "Continue"
Set-Location $PSScriptRoot
$env:Path = "C:\Program Files\Go\bin;$env:USERPROFILE\go\bin;" + $env:Path

$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
foreach ($arch in "amd64", "arm64") {
    $env:GOARCH = $arch
    Write-Host "agent linux/$arch"
    go build -trimpath -ldflags "-s -w" -o "agentbin\dist\backupper-agent-linux-$arch" .\cmd\agent
    if ($LASTEXITCODE -ne 0) { exit 1 }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

Write-Host "panel"
wails build -clean -trimpath -ldflags "-s -w"
if ($LASTEXITCODE -ne 0) { exit 1 }
Write-Host "OK: build\bin\Backupper.exe"

