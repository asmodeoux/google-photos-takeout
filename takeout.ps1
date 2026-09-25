# One command on Windows: check tools, build, and run takeout.
# Start it with .\takeout.cmd, which runs this script even where PowerShell
# scripts are blocked by the execution policy.
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# Installed, but this window started before the installer changed PATH.
function Test-InstalledElsewhere([string[]]$Patterns) {
    foreach ($p in $Patterns) {
        if (Get-ChildItem -Path $p -ErrorAction SilentlyContinue | Select-Object -First 1) { return $true }
    }
    return $false
}

$winget = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Packages"
$missing = $false
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    if (Test-InstalledElsewhere @("$env:ProgramFiles\Go\bin\go.exe", "$winget\GoLang.Go*\*\go.exe")) {
        Write-Host "Go is installed but not on PATH in this window. Open a new PowerShell window and run the same command."
    } else {
        Write-Host "Go is required to build takeout. Install it with:"
        Write-Host "  winget install --id GoLang.Go -e"
        Write-Host "then open a new PowerShell window. Or download a prebuilt takeout.exe (see README.md)."
    }
    $missing = $true
}
if (-not (Get-Command exiftool -ErrorAction SilentlyContinue)) {
    if (Test-InstalledElsewhere @("$env:ProgramFiles\ExifTool\exiftool.exe", "$winget\OliverBetz.ExifTool*\*\exiftool.exe")) {
        Write-Host "ExifTool is installed but not on PATH in this window. Open a new PowerShell window and run the same command."
    } else {
        Write-Host "ExifTool is required. Install it with:"
        Write-Host "  winget install --id OliverBetz.ExifTool -e"
        Write-Host "then open a new PowerShell window."
    }
    $missing = $true
}
if ($missing) { exit 2 }
if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Host "note: ffmpeg is optional. Without it, WebM and MKV videos go to results\not-importable. Install with: winget install --id Gyan.FFmpeg -e"
}

& go build -o takeout.exe ./cmd/takeout
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$env:TAKEOUT_LAUNCHER = ".\takeout.cmd"

# Before a run, check this computer the same way "takeout doctor" does.
if ($args.Count -gt 0 -and $args[0] -eq "run") {
    $doctorArgs = @("doctor")
    for ($i = 1; $i -lt $args.Count; $i++) {
        $a = [string]$args[$i]
        foreach ($f in @("--results", "--exiftool", "--ffmpeg", "-results", "-exiftool", "-ffmpeg")) {
            if ($a -eq $f -and $i + 1 -lt $args.Count) { $doctorArgs += @($a, [string]$args[$i + 1]) }
            elseif ($a.StartsWith("$f=")) { $doctorArgs += $a }
        }
    }
    & .\takeout.exe @doctorArgs
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
& .\takeout.exe @args
exit $LASTEXITCODE
