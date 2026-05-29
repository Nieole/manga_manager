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

# 显式锁定 Windows/amd64 目标平台，避免被会话级 GOOS/GOARCH 覆盖
# 项目通过 chai2010/webp 依赖 CGO，因此沿用环境默认的 CGO_ENABLED（Windows 本机构建一般为 1）
$prevGOOS = $env:GOOS
$prevGOARCH = $env:GOARCH

Push-Location $repoRoot
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"

    Write-Host "  GOOS=$env:GOOS GOARCH=$env:GOARCH"
    go build -trimpath -o $binaryPath .\cmd\server
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
}
finally {
    $env:GOOS = $prevGOOS
    $env:GOARCH = $prevGOARCH
    Pop-Location
}

Write-Host "Build completed: $binaryPath"
