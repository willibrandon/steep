<#
.SYNOPSIS
    Remove PostgreSQL replication test environment

.DESCRIPTION
    Stops and removes Docker containers, volumes, and network created by repl-test-setup.ps1

.PARAMETER KeepVolumes
    Don't remove Docker volumes (preserve data)

.PARAMETER Force
    Don't prompt for confirmation

.EXAMPLE
    .\repl-test-teardown.ps1
    Remove all containers with confirmation

.EXAMPLE
    .\repl-test-teardown.ps1 -Force
    Remove all containers without confirmation

.EXAMPLE
    .\repl-test-teardown.ps1 -KeepVolumes
    Remove containers but keep volume data
#>

[CmdletBinding()]
param(
    [switch]$KeepVolumes,
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$NetworkName = "steep-repl-test"
$ContainerPrefix = "steep-pg-"

# Colors
function Write-Info { Write-Host "[INFO] $args" -ForegroundColor Blue }
function Write-Success { Write-Host "[OK] $args" -ForegroundColor Green }
function Write-Warn { Write-Host "[WARN] $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "[ERROR] $args" -ForegroundColor Red }

# Find all related containers
$containers = docker ps -a --filter "name=$ContainerPrefix" --format "{{.Names}}" 2>$null

if (-not $containers) {
    Write-Info "No test containers found"

    # Still try to clean up network
    $networks = docker network ls --format "{{.Name}}" 2>$null | Where-Object { $_ -eq $NetworkName }
    if ($networks) {
        Write-Info "Removing network: $NetworkName"
        docker network rm $NetworkName 2>$null | Out-Null
        Write-Success "Network removed"
    }

    exit 0
}

# Show what will be removed
Write-Host "The following containers will be removed:"
foreach ($container in $containers) {
    Write-Host "  - $container"
}

# Find related volumes
$volumes = docker volume ls --filter "name=steep-replica" --format "{{.Name}}" 2>$null
if ($volumes -and -not $KeepVolumes) {
    Write-Host ""
    Write-Host "The following volumes will be removed:"
    foreach ($volume in $volumes) {
        Write-Host "  - $volume"
    }
}

Write-Host ""

# Confirm unless -Force
if (-not $Force) {
    $reply = Read-Host "Continue? [y/N]"
    if ($reply -notmatch "^[Yy]$") {
        Write-Info "Aborted"
        exit 0
    }
}

# Stop containers
Write-Info "Stopping containers..."
foreach ($container in $containers) {
    docker stop $container 2>$null | Out-Null
}
Write-Success "Containers stopped"

# Remove containers
Write-Info "Removing containers..."
foreach ($container in $containers) {
    docker rm $container 2>$null | Out-Null
}
Write-Success "Containers removed"

# Remove volumes
if (-not $KeepVolumes -and $volumes) {
    Write-Info "Removing volumes..."
    foreach ($volume in $volumes) {
        docker volume rm $volume 2>$null | Out-Null
    }
    Write-Success "Volumes removed"
} elseif ($KeepVolumes) {
    Write-Info "Keeping volumes (use 'docker volume rm' to remove manually)"
}

# Remove network
$networks = docker network ls --format "{{.Name}}" 2>$null | Where-Object { $_ -eq $NetworkName }
if ($networks) {
    Write-Info "Removing network: $NetworkName"
    docker network rm $NetworkName 2>$null | Out-Null
    Write-Success "Network removed"
}

Write-Host ""
Write-Success "Teardown complete!"
