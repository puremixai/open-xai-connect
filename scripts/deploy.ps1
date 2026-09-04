[CmdletBinding()]
# Deploy the current committed HEAD without copying local secrets or untracked files.
# Example: .\scripts\deploy.ps1 -SshKeyPath D:\bbs\ssh-key-2024-08-02.key
# Add -DryRun to validate the target and local worktree without contacting the server.
param(
    [string]$RemoteHost = "150.230.252.210",
    [string]$RemoteUser = "ubuntu",
    [string]$SshKeyPath = $env:XAI_CONNECT_DEPLOY_KEY,
    [int]$SshPort = 22,
    [string]$RemoteDir = "/home/ubuntu/app/xai-connect",
    [int]$HealthTimeoutSeconds = 120,
    [int]$HealthIntervalSeconds = 5,
    [switch]$SkipTests,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Invoke-CheckedNative {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [string[]]$Arguments = @()
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath failed with exit code $LASTEXITCODE"
    }
}

function Quote-PosixSingleQuoted {
    param([Parameter(Mandatory = $true)][string]$Value)

    return "'" + $Value.Replace("'", "'\''") + "'"
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$requiredTools = @("git", "ssh", "scp")
if (-not $SkipTests) {
    $requiredTools += "go"
}
foreach ($tool in $requiredTools) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "required command not found: $tool"
    }
}

if ([string]::IsNullOrWhiteSpace($SshKeyPath)) {
    throw "SshKeyPath is required; pass -SshKeyPath or set XAI_CONNECT_DEPLOY_KEY"
}
if ($SshPort -lt 1 -or $SshPort -gt 65535) {
    throw "SshPort must be between 1 and 65535"
}
if ($HealthTimeoutSeconds -lt 1 -or $HealthIntervalSeconds -lt 1) {
    throw "health timeout and interval must be positive"
}

$keyPath = (Resolve-Path -LiteralPath $SshKeyPath -ErrorAction Stop).Path
if ((Get-Item -LiteralPath $keyPath).PSIsContainer) {
    throw "SshKeyPath must point to a file: $keyPath"
}

Push-Location $repoRoot
$archivePath = $null
try {
    $trackedStatus = @(git status --porcelain --untracked-files=no)
    if ($LASTEXITCODE -ne 0) {
        throw "unable to inspect the Git worktree"
    }
    if ($trackedStatus.Count -gt 0) {
        throw "tracked files have uncommitted changes; commit them before deploying:`n$($trackedStatus -join [Environment]::NewLine)"
    }

    $commit = (& git rev-parse --short=7 HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) {
        throw "unable to resolve HEAD commit"
    }
    Invoke-CheckedNative -FilePath "git" -Arguments @("diff", "--check", "HEAD", "--")

    if ($SkipTests) {
        Write-Output "go tests skipped"
    }
    else {
        Write-Output "running go tests..."
        Invoke-CheckedNative -FilePath "go" -Arguments @("test", "./...", "-count=1")
        Write-Output "go tests passed"
    }

    $remoteArchive = "/tmp/xai-connect-$commit.tar.gz"
    $remoteTarget = "${RemoteUser}@${RemoteHost}:$remoteArchive"

    if ($DryRun) {
        Write-Output "DRY_RUN"
        Write-Output "commit=$commit"
        Write-Output "target=${RemoteUser}@${RemoteHost}:$RemoteDir"
        Write-Output "would upload archive: $remoteArchive"
        Write-Output "would run docker compose: build portal; up -d --no-deps portal"
        Write-Output "would wait for portal health and write .release"
        return
    }

    $artifactName = "xai-connect-$commit-$PID.tar.gz"
    $archivePath = Join-Path ([System.IO.Path]::GetTempPath()) $artifactName
    Invoke-CheckedNative -FilePath "git" -Arguments @("archive", "--format=tar.gz", "--output=$archivePath", "HEAD")
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "Git archive was not created: $archivePath"
    }

    $scpArguments = @(
        "-i", $keyPath,
        "-P", "$SshPort",
        "-o", "BatchMode=yes",
        "-o", "IdentitiesOnly=yes",
        "-o", "StrictHostKeyChecking=accept-new",
        $archivePath,
        $remoteTarget
    )
    Invoke-CheckedNative -FilePath "scp" -Arguments $scpArguments

    $remoteScript = @'
set -eu
remote_dir=__REMOTE_DIR__
remote_archive=__REMOTE_ARCHIVE__
release=__RELEASE__
health_timeout=__HEALTH_TIMEOUT__
health_interval=__HEALTH_INTERVAL__
compose_file="$remote_dir/deploy/docker-compose.yml"

test -d "$remote_dir"
test -s "$remote_dir/.env"
cd "$remote_dir"
trap 'rm -f -- "$remote_archive"' EXIT

tar -xzf "$remote_archive" -C "$remote_dir"
docker compose --env-file "$remote_dir/.env" -f "$compose_file" config --quiet
docker compose --env-file "$remote_dir/.env" -f "$compose_file" exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "$remote_dir/migrations/003_application_pkce_nonce.sql"
docker compose --env-file "$remote_dir/.env" -f "$compose_file" build portal
docker compose --env-file "$remote_dir/.env" -f "$compose_file" up -d --no-deps portal

container_id="$(docker compose --env-file "$remote_dir/.env" -f "$compose_file" ps -q portal)"
if [ -z "$container_id" ]; then
  echo "portal container was not created" >&2
  exit 1
fi

deadline=$((SECONDS + health_timeout))
health="unknown"
while [ "$SECONDS" -lt "$deadline" ]; do
  health="$(docker inspect --format '{{.State.Health.Status}}' "$container_id" 2>/dev/null || true)"
  if [ "$health" = "healthy" ]; then
    break
  fi
  if [ "$health" = "unhealthy" ]; then
    echo "portal container became unhealthy" >&2
    docker logs --tail 80 "$container_id" >&2 || true
    exit 1
  fi
  sleep "$health_interval"
done

if [ "$health" != "healthy" ]; then
  echo "portal health check timed out: $health" >&2
  docker logs --tail 80 "$container_id" >&2 || true
  exit 1
fi

printf '%s\n' "$release" > "$remote_dir/.release"
docker compose --env-file "$remote_dir/.env" -f "$compose_file" ps portal
echo "DEPLOY_OK release=$release"
'@
    $remoteScript = $remoteScript.Replace("__REMOTE_DIR__", (Quote-PosixSingleQuoted $RemoteDir))
    $remoteScript = $remoteScript.Replace("__REMOTE_ARCHIVE__", (Quote-PosixSingleQuoted $remoteArchive))
    $remoteScript = $remoteScript.Replace("__RELEASE__", (Quote-PosixSingleQuoted $commit))
    $remoteScript = $remoteScript.Replace("__HEALTH_TIMEOUT__", "$HealthTimeoutSeconds")
    $remoteScript = $remoteScript.Replace("__HEALTH_INTERVAL__", "$HealthIntervalSeconds")

    $sshArguments = @(
        "-i", $keyPath,
        "-p", "$SshPort",
        "-o", "BatchMode=yes",
        "-o", "IdentitiesOnly=yes",
        "-o", "StrictHostKeyChecking=accept-new",
        "${RemoteUser}@${RemoteHost}",
        "sed '1s/^\xEF\xBB\xBF//' | bash -s"
    )
    $previousOutputEncoding = $OutputEncoding
    try {
        $OutputEncoding = [System.Text.UTF8Encoding]::new($false)
        $remoteScript | & ssh @sshArguments
        if ($LASTEXITCODE -ne 0) {
            throw "remote deployment failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        $OutputEncoding = $previousOutputEncoding
    }

    Write-Output "deployed commit $commit to ${RemoteUser}@${RemoteHost}:$RemoteDir"
}
finally {
    if ($archivePath -and (Test-Path -LiteralPath $archivePath)) {
        Remove-Item -LiteralPath $archivePath -Force -ErrorAction SilentlyContinue
    }
    Pop-Location
}
