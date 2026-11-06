param([switch]$NoBrowser)

$ErrorActionPreference = 'Stop'
$artexRoot = Split-Path -Parent $PSScriptRoot
$artexUrl = 'http://localhost:8787'
$artexLogDir = Join-Path $env:LOCALAPPDATA 'ARTEX'
New-Item -ItemType Directory -Path $artexLogDir -Force | Out-Null
$artexLog = Join-Path $artexLogDir 'launcher.log'

function Write-LauncherLog([string]$Message) {
    Add-Content -LiteralPath $artexLog -Encoding UTF8 -Value ('[{0}] {1}' -f (Get-Date -Format s), $Message)
}

function Test-ARTEX {
    try {
        $artexHealth = Invoke-RestMethod -Uri 'http://127.0.0.1:8787/api/health' -TimeoutSec 3
        return ($artexHealth.ok -eq $true -and $artexHealth.service -eq 'artex')
    } catch { return $false }
}

function Test-DockerReady([string]$DockerPath) {
    $artexPreviousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & $DockerPath --context desktop-linux info --format '{{.ServerVersion}}' *> $null
        return ($LASTEXITCODE -eq 0)
    } finally { $ErrorActionPreference = $artexPreviousPreference }
}

try {
    if (-not (Test-Path -LiteralPath (Join-Path $artexRoot 'docker-compose.override.yml'))) {
        throw 'The local OAuth installation override is missing. Restore it before starting ARTEX.'
    }
    & {
        $artexDocker = (Get-Command docker.exe -ErrorAction SilentlyContinue).Source
        if (-not $artexDocker) {
            $artexDocker = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe'
        }
        if (-not (Test-Path -LiteralPath $artexDocker)) { throw 'Docker Desktop is not installed.' }

        if (-not (Test-DockerReady $artexDocker)) {
            $artexDockerDesktop = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\Docker Desktop.exe'
            if (-not (Test-Path -LiteralPath $artexDockerDesktop)) {
                $artexDockerDesktop = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
            }
            if (-not (Test-Path -LiteralPath $artexDockerDesktop)) { throw 'Docker Desktop could not be found.' }
            Write-LauncherLog 'Starting Docker Desktop.'
            Start-Process -FilePath $artexDockerDesktop -WindowStyle Hidden
            $artexDeadline = (Get-Date).AddSeconds(150)
            do {
                Start-Sleep -Seconds 2
                if (Test-DockerReady $artexDocker) { break }
            } while ((Get-Date) -lt $artexDeadline)
            if (-not (Test-DockerReady $artexDocker)) { throw 'Docker Desktop did not become ready. Open Docker Desktop and retry.' }
        }

        Push-Location -LiteralPath $artexRoot
        try {
            $ErrorActionPreference = 'Continue'
            try {
                $artexComposeOutput = & $artexDocker --context desktop-linux compose up -d 2>&1
                $artexComposeExit = $LASTEXITCODE
            } finally { $ErrorActionPreference = 'Stop' }
            $artexComposeOutput | ForEach-Object { Write-LauncherLog ([string]$_) }
            if ($artexComposeExit -ne 0) { throw 'ARTEX could not start. See the launcher log.' }
        } finally { Pop-Location }

        $artexDeadline = (Get-Date).AddSeconds(90)
        while (-not (Test-ARTEX)) {
            if ((Get-Date) -ge $artexDeadline) { throw 'ARTEX did not become ready. See the launcher log.' }
            Start-Sleep -Seconds 2
        }
    }
    Write-LauncherLog 'ARTEX is ready at http://localhost:8787.'
    if (-not $NoBrowser) { Start-Process $artexUrl }
} catch {
    Write-LauncherLog $_.Exception.Message
    if ($NoBrowser) { throw }
    Add-Type -AssemblyName System.Windows.Forms
    [System.Windows.Forms.MessageBox]::Show(
        ($_.Exception.Message + "`r`n`r`nLog: " + $artexLog),
        'ARTEX', 'OK', 'Error') | Out-Null
    exit 1
}
