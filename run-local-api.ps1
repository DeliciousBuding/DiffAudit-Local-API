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
