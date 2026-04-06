param(
    [string]$ListenHost = "127.0.0.1",
    [string]$ListenPort = "8765",
    [string]$ProjectRoot,
    [string]$ExperimentsRoot,
    [string]$JobsRoot,
    [string]$GpuScheduler,
    [string]$GpuRequestDoc,
    [string]$GpuAgentPrefix = "local-api"
)

$ErrorActionPreference = "Stop"

function Write-LogLine {
    param(
        [string]$Level,
        [string]$Message,
        [ConsoleColor]$Color = [ConsoleColor]::Gray
    )

    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "[$timestamp] [$Level] $Message" -ForegroundColor $Color
}

function Write-Section {
    param(
        [string]$Title
    )

    Write-Host ""
    Write-Host ("=" * 72) -ForegroundColor DarkGray
    Write-Host (" DiffAudit Local API :: " + $Title) -ForegroundColor Cyan
    Write-Host ("=" * 72) -ForegroundColor DarkGray
}

$serviceRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$workspaceRoot = [System.IO.Path]::GetFullPath((Join-Path $serviceRoot "..\\.."))

if (-not $ProjectRoot) {
    $ProjectRoot = Join-Path $workspaceRoot "Project"
}

if (-not $ExperimentsRoot) {
    $ExperimentsRoot = Join-Path $ProjectRoot "experiments"
}

if (-not $JobsRoot) {
    $JobsRoot = Join-Path $ProjectRoot "workspaces\\local-api\\jobs"
}

$localOpsRoot = Join-Path $workspaceRoot "LocalOps"

if (-not $GpuScheduler) {
    $GpuScheduler = Join-Path $localOpsRoot "paper-resource-scheduler\\gpu-scheduler.exe"
}

if (-not $GpuRequestDoc) {
    $GpuRequestDoc = Join-Path $localOpsRoot "paper-resource-scheduler\\gpu-resource-requests.md"
}

$host.UI.RawUI.WindowTitle = "DiffAudit Local API"

Write-Section "Startup"
Write-LogLine "INFO" "Preparing local control plane launcher" Cyan
Write-LogLine "INFO" "Service root: $serviceRoot"
Write-LogLine "INFO" "Workspace root: $workspaceRoot"
Write-LogLine "INFO" "Listen address: $ListenHost`:$ListenPort" Green

Write-Section "Resolved Paths"
Write-LogLine "INFO" "Project root: $ProjectRoot"
Write-LogLine "INFO" "Experiments root: $ExperimentsRoot"
Write-LogLine "INFO" "Jobs root: $JobsRoot"
Write-LogLine "INFO" "GPU scheduler: $GpuScheduler"
Write-LogLine "INFO" "GPU request doc: $GpuRequestDoc"
Write-LogLine "INFO" "GPU agent prefix: $GpuAgentPrefix"

Write-Section "Launch"
Write-LogLine "INFO" "Starting Go Local API service. Runtime logs will continue below." Yellow

Push-Location $serviceRoot
try {
    go run ./cmd/local-api `
        --host $ListenHost `
        --port $ListenPort `
        --project-root $ProjectRoot `
        --experiments-root $ExperimentsRoot `
        --jobs-root $JobsRoot `
        --gpu-scheduler $GpuScheduler `
        --gpu-request-doc $GpuRequestDoc `
        --gpu-agent-prefix $GpuAgentPrefix
}
finally {
    Pop-Location
}
