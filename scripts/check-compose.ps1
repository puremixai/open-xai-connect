param(
    [string]$ComposeFile = (Join-Path $PSScriptRoot "..\deploy\docker-compose.yml")
)

$ErrorActionPreference = "Stop"
$content = Get-Content -LiteralPath $ComposeFile -Raw -Encoding UTF8

if ($content -notmatch "oryd/hydra:v26\.3\.10") {
    throw "Hydra image must be pinned to v26.3.10"
}
if ($content -match '(?m)^\s*-\s*[^\r\n]*:4445:4445') {
    throw "Hydra admin port 4445 must not be published"
}
if ($content -notlike '*SECRETS_SYSTEM: ${HYDRA_SYSTEM_SECRET:?set*') {
    throw "Hydra system secret must be required from the environment"
}
if ($content -notlike '*CONNECT_ENCRYPTION_KEY: ${CONNECT_ENCRYPTION_KEY:?set*') {
    throw "Connect encryption key must be required from the environment"
}
foreach ($line in ($content -split "`r?`n")) {
    if ($line -match '(?i)^\s*[A-Z0-9_]*(PASSWORD|SECRET):\s*' -and $line -notmatch '\$\{') {
        throw "Compose contains a possible hard-coded secret"
    }
}

Write-Output "compose checks passed: Hydra admin remains internal and required secrets are externalized"
