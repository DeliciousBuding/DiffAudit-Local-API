param(
    [string]$EnvFile,
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

function Import-EnvFile {
    param(
        [string]$Path
    )

    if (-not $Path) {
        return
    }

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Env file not found: $Path"
    }

    Get-Content -LiteralPath $Path | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith("#")) {
            return
        }

        $pair = $line -split "=", 2
        if ($pair.Count -ne 2) {
            return
        }

        $name = $pair[0].Trim()
        $value = $pair[1].Trim()
        Set-Item -Path ("Env:" + $name) -Value $value
    }
}

$serviceRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$workspaceRoot = [System.IO.Path]::GetFullPath((Join-Path $serviceRoot "..\\.."))

if ($EnvFile) {
    Import-EnvFile -Path $EnvFile
}

if (-not $ProjectRoot) {
    $ProjectRoot = $env:DIFFAUDIT_LOCAL_API_PROJECT_ROOT
}

if (-not $ProjectRoot) {
    $ProjectRoot = Join-Path $workspaceRoot "Project"
}

if (-not $ExperimentsRoot) {
    $ExperimentsRoot = $env:DIFFAUDIT_LOCAL_API_EXPERIMENTS_ROOT
}

if (-not $ExperimentsRoot) {
    $ExperimentsRoot = Join-Path $ProjectRoot "experiments"
}

if (-not $JobsRoot) {
    $JobsRoot = $env:DIFFAUDIT_LOCAL_API_JOBS_ROOT
}

if (-not $JobsRoot) {
    $JobsRoot = Join-Path $ProjectRoot "workspaces\\local-api\\jobs"
}

$localOpsRoot = Join-Path $workspaceRoot "LocalOps"

if (-not $GpuScheduler) {
    $GpuScheduler = $env:DIFFAUDIT_LOCAL_API_GPU_SCHEDULER
}

if (-not $GpuScheduler) {
    $GpuScheduler = Join-Path $localOpsRoot "paper-resource-scheduler\\gpu-scheduler.exe"
}

if (-not $GpuRequestDoc) {
    $GpuRequestDoc = $env:DIFFAUDIT_LOCAL_API_GPU_REQUEST_DOC
}

if (-not $GpuRequestDoc) {
    $GpuRequestDoc = Join-Path $localOpsRoot "paper-resource-scheduler\\gpu-resource-requests.md"
}

if ($GpuAgentPrefix -eq "local-api" -and $env:DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX) {
    $GpuAgentPrefix = $env:DIFFAUDIT_LOCAL_API_GPU_AGENT_PREFIX
}

if ($ListenHost -eq "127.0.0.1" -and $env:DIFFAUDIT_LOCAL_API_HOST) {
    $ListenHost = $env:DIFFAUDIT_LOCAL_API_HOST
}

if ($ListenPort -eq "8765" -and $env:DIFFAUDIT_LOCAL_API_PORT) {
    $ListenPort = $env:DIFFAUDIT_LOCAL_API_PORT
}

$host.UI.RawUI.WindowTitle = "DiffAudit Local API"

Write-Section "Startup"
Write-LogLine "INFO" "Preparing local control plane launcher" Cyan
if ($EnvFile) {
    Write-LogLine "INFO" "Loaded env file: $EnvFile" Yellow
}
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
