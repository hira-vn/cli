# Hira installer for Windows — one command to get started.
#
# Install CLI (default): connects to hira.vn
#   irm https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.ps1 | iex
#
# Self-host: starts a local Hira server + installs CLI + configures
#   $env:HIRA_MODE="local"; irm https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.ps1 | iex
#

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$RepoUrl       = "https://github.com/hira-vn/cli.git"
$RepoWebUrl    = "https://github.com/hira-vn/cli"
$DefaultInstallDir = Join-Path $env:USERPROFILE ".hira\server"
$InstallDir    = if ($env:HIRA_INSTALL_DIR) { $env:HIRA_INSTALL_DIR } else { $DefaultInstallDir }

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$Msg) Write-Host "==> $Msg" -ForegroundColor Cyan }
function Write-Ok    { param([string]$Msg) Write-Host "[OK] $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Warning $Msg }
function Write-Fail  { param([string]$Msg) Write-Host "[ERROR] $Msg" -ForegroundColor Red; exit 1 }

function Test-CommandExists {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/hira-vn/cli/releases/latest" -ErrorAction Stop
        return $release.tag_name
    } catch {
        return $null
    }
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
function Install-CliBinary {
    Write-Info "Installing Hira CLI from GitHub Releases..."

    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Fail "Hira requires a 64-bit Windows installation."
    }
    $arch = "amd64"

    $latest = Get-LatestVersion
    if (-not $latest) {
        Write-Fail "Could not determine latest release. Check your network connection."
    }

    $url = "https://github.com/hira-vn/cli/releases/download/$latest/hira_windows_$arch.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "hira-install"

    if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    Write-Info "Downloading $url ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir "hira.zip") -UseBasicParsing
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to download CLI binary: $_"
    }

    # Verify SHA256 checksum
    $checksumUrl = "https://github.com/hira-vn/cli/releases/download/$latest/checksums.txt"
    try {
        $checksums = Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing -ErrorAction Stop
        $zipFile = Join-Path $tmpDir "hira.zip"
        $actualHash = (Get-FileHash -Path $zipFile -Algorithm SHA256).Hash.ToLower()
        $expectedLine = ($checksums.Content -split "`n") | Where-Object { $_ -match "hira_windows_$arch\.zip" } | Select-Object -First 1
        if ($expectedLine) {
            $expectedHash = ($expectedLine -split "\s+")[0].ToLower()
            if ($actualHash -ne $expectedHash) {
                Remove-Item $tmpDir -Recurse -Force
                Write-Fail "Checksum verification failed. Expected: $expectedHash, Got: $actualHash"
            }
            Write-Ok "Checksum verified"
        } else {
            Write-Warn "Could not find checksum entry for windows_$arch — skipping verification."
        }
    } catch {
        Write-Warn "Could not download checksums.txt — skipping verification."
    }

    Expand-Archive -Path (Join-Path $tmpDir "hira.zip") -DestinationPath $tmpDir -Force

    $binDir = Join-Path $env:USERPROFILE ".hira\bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    $exeSrc = Join-Path $tmpDir "hira.exe"
    if (-not (Test-Path $exeSrc)) {
        $exeSrc = Get-ChildItem -Path $tmpDir -Filter "hira.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
    }
    if (-not $exeSrc -or -not (Test-Path $exeSrc)) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "hira.exe not found in downloaded archive."
    }

    Copy-Item $exeSrc (Join-Path $binDir "hira.exe") -Force
    Remove-Item $tmpDir -Recurse -Force

    Add-ToUserPath $binDir
    Write-Ok "Hira CLI installed to $binDir\hira.exe"
}

function Add-ToUserPath {
    param([string]$Dir)
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -and $currentPath.Split(";") -contains $Dir) {
        return
    }
    $newPath = if ($currentPath) { "$currentPath;$Dir" } else { $Dir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    # Also update current session
    if ($env:Path -notlike "*$Dir*") {
        $env:Path = "$Dir;$env:Path"
    }
    Write-Info "Added $Dir to user PATH (restart your terminal for other sessions to pick it up)."
}

function Install-Cli {
    if (Test-CommandExists "hira") {
        $currentVer = (hira version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        $latestVer = Get-LatestVersion

        $currentCmp = $currentVer -replace '^v',''
        $latestCmp = if ($latestVer) { $latestVer -replace '^v','' } else { $null }

        $isUpToDate = -not $latestCmp
        if (-not $isUpToDate) {
            try {
                $isUpToDate = [System.Version]$currentCmp -ge [System.Version]$latestCmp
            } catch {
                $isUpToDate = $currentCmp -eq $latestCmp
            }
        }

        if ($isUpToDate) {
            Write-Ok "Hira CLI is up to date ($currentVer)"
            return
        }

        Write-Info "Hira CLI $currentVer installed, latest is $latestVer - upgrading..."
        Install-CliBinary

        $newVer = (hira version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        Write-Ok "Hira CLI upgraded ($currentVer -> $newVer)"
        return
    }

    Install-CliBinary

    if (-not (Test-CommandExists "hira")) {
        Write-Fail "CLI installed but 'hira' not found on PATH. Restart your terminal and try again."
    }
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
function Test-Docker {
    if (-not (Test-CommandExists "docker")) {
        Write-Fail @"
Docker is not installed. Hira self-hosting requires Docker and Docker Compose.

Install Docker Desktop for Windows:
  https://docs.docker.com/desktop/install/windows-install/

After installing Docker, re-run this script with `$env:HIRA_MODE="local"`.
"@
    }

    try {
        docker info 2>$null | Out-Null
    } catch {
        Write-Fail "Docker is installed but not running. Please start Docker Desktop and re-run this script."
    }

    Write-Ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / local)
# ---------------------------------------------------------------------------
function Install-Server {
    Write-Info "Setting up Hira server..."

    if (Test-Path (Join-Path $InstallDir ".git")) {
        Write-Info "Updating existing installation at $InstallDir..."
        Write-Warn "Any local changes in $InstallDir will be overwritten."
        Push-Location $InstallDir
        git fetch origin main --depth 1 2>$null
        git reset --hard origin/main 2>$null
        Pop-Location
    } else {
        Write-Info "Cloning Hira repository..."
        if (-not (Test-CommandExists "git")) {
            Write-Fail "Git is not installed. Please install git and re-run."
        }
        if (Test-Path $InstallDir) {
            Write-Warn "Removing incomplete installation at $InstallDir..."
            Remove-Item $InstallDir -Recurse -Force
        }
        $parentDir = Split-Path $InstallDir -Parent
        if (-not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        git clone --depth 1 $RepoUrl $InstallDir
    }

    Write-Ok "Repository ready at $InstallDir"

    Push-Location $InstallDir

    if (-not (Test-Path ".env")) {
        Write-Info "Creating .env with random JWT_SECRET..."
        Copy-Item ".env.example" ".env"
        $jwt = -join ((1..32) | ForEach-Object { "{0:x2}" -f (Get-Random -Maximum 256) })
        (Get-Content ".env") -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwt" | Set-Content ".env"
        Write-Ok "Generated .env with random JWT_SECRET"
    } else {
        Write-Ok "Using existing .env"
    }

    Write-Info "Starting Hira services (this may take a few minutes on first run)..."
    docker compose -f docker-compose.selfhost.yml up -d --build

    Write-Info "Waiting for backend to be ready..."
    $ready = $false
    for ($i = 1; $i -le 45; $i++) {
        try {
            $null = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
            $ready = $true
            break
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if ($ready) {
        Write-Ok "Hira server is running"
    } else {
        Write-Warn "Server is still starting. Check logs with:"
        Write-Host "  cd $InstallDir; docker compose -f docker-compose.selfhost.yml logs"
    }

    Pop-Location
}


# ---------------------------------------------------------------------------
# Main: Default mode (cloud)
# ---------------------------------------------------------------------------
function Start-DefaultInstall {
    Write-Host ""
    Write-Host "  Hira - Installer" -ForegroundColor White
    Write-Host ""

    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Hira CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Next: configure your environment"
    Write-Host ""
    Write-Host "     hira setup               " -NoNewline; Write-Host "# Connect to Hira Cloud (hira.vn)" -ForegroundColor DarkGray
    Write-Host "     hira setup self-host      " -NoNewline; Write-Host "# Connect to a self-hosted server" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Self-hosting? Install the server first:"
    Write-Host '     $env:HIRA_MODE="with-server"; irm https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Main: Local mode (self-host)
# ---------------------------------------------------------------------------
function Start-LocalInstall {
    Write-Host ""
    Write-Host "  Hira - Self-Host Installer" -ForegroundColor White
    Write-Host "  Provisioning server infrastructure + installing CLI"
    Write-Host ""

    Test-Docker
    Install-Server
    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Hira server is running and CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Frontend:  http://localhost:3000"
    Write-Host "  Backend:   http://localhost:8080"
    Write-Host "  Server at: $InstallDir"
    Write-Host ""
    Write-Host "  Next: configure your CLI to connect"
    Write-Host ""
    Write-Host "     hira setup self-host  " -NoNewline; Write-Host "# Configure + authenticate + start daemon" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Login: configure RESEND_API_KEY in .env for email codes,"
    Write-Host "  or set APP_ENV=development in .env to enable the dev master code 888888."
    Write-Host ""
    Write-Host "  To stop all services:"
    Write-Host '     $env:HIRA_MODE="stop"; irm https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
function Start-Stop {
    Write-Host ""
    Write-Info "Stopping Hira services..."

    if (Test-Path $InstallDir) {
        Push-Location $InstallDir
        if (Test-Path "docker-compose.selfhost.yml") {
            docker compose -f docker-compose.selfhost.yml down
            Write-Ok "Docker services stopped"
        } else {
            Write-Warn "No docker-compose.selfhost.yml found at $InstallDir"
        }
        Pop-Location
    } else {
        Write-Warn "No Hira installation found at $InstallDir"
    }

    if (Test-CommandExists "hira") {
        try {
            hira daemon stop 2>$null
            Write-Ok "Daemon stopped"
        } catch {}
    }

    Write-Host ""
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
$mode = if ($env:HIRA_MODE) { $env:HIRA_MODE.ToLower() } else { "default" }

switch ($mode) {
    "with-server" { Start-LocalInstall }
    "local"       { Start-LocalInstall }  # backwards compat alias
    "stop"        { Start-Stop }
    default       { Start-DefaultInstall }
}
