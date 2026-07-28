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
        # npm ci 而非 npm install：见 build.sh 同处注释——install 会就地改写 package-lock.json，
        # 使本地构建的依赖树与 CI 不可复现地分叉。
        npm ci
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

# 版本信息注入：不注入则二进制的「关于」页与日志恒为 dev/unknown，与 release 产物不可区分。
$version = if ($env:VERSION) { $env:VERSION } else {
    $described = (git describe --tags --always --dirty 2>$null)
    if ($LASTEXITCODE -eq 0 -and $described) { $described } else { "dev" }
}
$commit = (git rev-parse HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $commit) { $commit = "unknown" }
$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$versionFlags = "-X 'main.Version=$version' -X 'main.Commit=$commit' -X 'main.BuildTime=$buildTime'"
Write-Host "  Version: $version ($commit)"

# 显式锁定 Windows/amd64 目标平台，避免被会话级 GOOS/GOARCH 覆盖
# 项目通过 chai2010/webp 依赖 CGO，因此沿用环境默认的 CGO_ENABLED（Windows 本机构建一般为 1）
$prevGOOS = $env:GOOS
$prevGOARCH = $env:GOARCH

Push-Location $repoRoot
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"

    Write-Host "  GOOS=$env:GOOS GOARCH=$env:GOARCH"
    go build -trimpath -ldflags $versionFlags -o $binaryPath .\cmd\server
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
