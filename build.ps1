param(
    [string]$OutputDir = "build",
    [string]$BinaryName = "manga-manager-win-amd64.exe",
    [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"

function Require-Command {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Missing required command: $Name"
    }
}

Write-Host "Checking build dependencies..."
Require-Command -Name "go"
Require-Command -Name "npm"

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$webDir = Join-Path $repoRoot "web"
$outputPath = Join-Path $repoRoot $OutputDir
$binaryPath = Join-Path $outputPath $BinaryName

if (-not $SkipFrontend) {
    Write-Host "Building frontend..."
    Push-Location $webDir
    try {
        npm install
        npm run build
    }
    finally {
        Pop-Location
    }
}
else {
    Write-Host "Skipping frontend build."
}

Write-Host "Building backend..."
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null

Push-Location $repoRoot
try {
    go build -o $binaryPath .\cmd\server
}
finally {
    Pop-Location
}

Write-Host "Build completed: $binaryPath"
