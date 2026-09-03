$ErrorActionPreference = "Stop"

$scriptPath = Join-Path $PSScriptRoot "deploy.ps1"
if (-not (Test-Path -LiteralPath $scriptPath -PathType Leaf)) {
    throw "deployment script is missing: $scriptPath"
}

$parseErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$null, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count -gt 0) {
    $parseMessages = @($parseErrors | ForEach-Object { $_.Message })
    throw "deployment script has PowerShell parse errors: $($parseMessages -join '; ')"
}

$source = Get-Content -LiteralPath $scriptPath -Raw -Encoding UTF8
foreach ($marker in @(
        "archive",
        "--format=tar.gz",
        "docker compose",
        "build portal",
        "up -d --no-deps portal",
        "State.Health.Status",
        ".release",
        "OutputEncoding",
        "UTF8Encoding",
        "sed '1s/^\\xEF\\xBB\\xBF//' | bash -s"
    )) {
    if ($source -notlike "*$marker*") {
        throw "deployment script is missing required step: $marker"
    }
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("xai-connect-deploy-test-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempRoot | Out-Null
try {
    $keyPath = Join-Path $tempRoot "test-key"
    New-Item -ItemType File -Path $keyPath | Out-Null
    $commit = (& git -C (Join-Path $PSScriptRoot "..") rev-parse --short HEAD).Trim()
    $output = @(& powershell -NoProfile -ExecutionPolicy Bypass -File $scriptPath -SshKeyPath $keyPath -DryRun 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "deployment script dry-run failed: $($output -join [Environment]::NewLine)"
    }
    $text = $output -join [Environment]::NewLine
    foreach ($marker in @("DRY_RUN", $commit, "go tests passed", "would upload archive", "would run docker compose")) {
        if ($text -notlike "*$marker*") {
            throw "deployment script dry-run is missing output: $marker`n$text"
        }
    }
}
finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output "deploy script checks passed"
