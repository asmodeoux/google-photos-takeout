# Runs the takeout binary on the synthetic corpus and checks the result.
# Usage: scripts\smoke.ps1 [-Binary path\to\takeout.exe] [-Results dir-to-keep]
param([string]$Binary = "", [string]$Results = "")
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

function Invoke-Checked {
    param([string]$Exe, [string[]]$Arguments)
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe $($Arguments -join ' ') exited $LASTEXITCODE" }
}

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("takeout-smoke-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $work | Out-Null
try {
    # The launcher must work in a fresh Windows PowerShell with the default
    # execution policy, where running .ps1 files directly is blocked.
    $launcher = (Resolve-Path ".\takeout.cmd").Path
    Invoke-Checked "powershell" @("-NoProfile", "-Command", "& '$launcher' version; exit `$LASTEXITCODE")

    if ($Binary -eq "") {
        $Binary = Join-Path $work "takeout.exe"
        Invoke-Checked "go" @("build", "-o", $Binary, "./cmd/takeout")
    }
    $archives = Join-Path $work "archives"
    $out = Join-Path $work "results"
    Invoke-Checked "go" @("run", "./cmd/testgen", $archives) | Out-Null
    $Binary = (Resolve-Path $Binary).Path
    if ($Results -ne "") { $Results = [System.IO.Path]::GetFullPath($Results) }

    # Relative folders, as with the defaults: archives\ and results\ here.
    Push-Location $work
    try {
        Invoke-Checked $Binary @("version")
        Invoke-Checked $Binary @("doctor")
        Invoke-Checked $Binary @("check")
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        Invoke-Checked $Binary @("run", "--default-tz", "Europe/Berlin", "--progress", "plain", "--quiet")
        Write-Host "run took $([int]$sw.Elapsed.TotalSeconds)s"
        Invoke-Checked $Binary @("verify")
        Invoke-Checked $Binary @("status")
    } finally {
        Pop-Location
    }

    $report = Get-Content (Join-Path $out ".takeout\report.json") -Raw | ConvertFrom-Json
    Write-Host "seconds: $($report.seconds | ConvertTo-Json -Compress)  files per second: $($report.files_per_second)  retries: $($report.retries | ConvertTo-Json -Compress)"
    if ($report.tag_errors -ne 0) { throw "tag errors: $($report.errors -join '; ')" }
    if ($report.live_pairs -ne 5) { throw "live pairs: $($report.live_pairs)" }
    if ($report.names_rule -ne "portable") { throw "names rule: $($report.names_rule)" }
    $leftover = Get-ChildItem -Path $out -Recurse -Filter "*_exiftool_tmp" -ErrorAction SilentlyContinue
    if ($leftover) { throw "ExifTool temp files left: $leftover" }
    if ($Results -ne "") {
        if (Test-Path $Results) { Remove-Item -Recurse -Force $Results }
        Copy-Item -Recurse $out $Results
    }
    Write-Host "smoke ok"
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
