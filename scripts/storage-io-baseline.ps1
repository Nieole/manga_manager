param(
    [Parameter(Mandatory = $true)]
    [string]$Library,

    [string]$Cache = ".\data\storage-io-bench",
    [string]$OutputDir = "docs\performance-baselines",
    [string]$Label = "external-hdd",

    [ValidateSet("auto", "ssd", "hdd_external", "network", "custom")]
    [string]$Profile = "hdd_external",

    [string]$Notes = "",

    [int]$MaxFiles = 300,
    [int]$ReadMB = 512,
    [int]$WriteFiles = 512,
    [int]$WriteKB = 64,
    [int]$CoverSamples = 128,
    [int]$CoverReadKB = 512,
    [int]$CoverWriteKB = 96,
    [int]$CoverConcurrency = 1,
    [int]$ReaderProbes = 40,
    [int]$ReaderKB = 256,
    [int]$BackgroundReaders = 2,

    [string]$CompareLibrary = "",
    [string]$CompareCache = "",
    [string]$CompareLabel = "internal-ssd",

    [ValidateSet("auto", "ssd", "hdd_external", "network", "custom")]
    [string]$CompareProfile = "ssd",

    [string]$CompareNotes = ""
)

$ErrorActionPreference = "Stop"

function Resolve-Directory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw "$Name does not exist or is not a directory: $Path"
    }
    return (Resolve-Path -LiteralPath $Path).Path
}

function New-Slug {
    param([string]$Value)

    $slug = ($Value -replace "[^A-Za-z0-9._-]+", "-").Trim("-")
    if ([string]::IsNullOrWhiteSpace($slug)) {
        return "storage-io"
    }
    return $slug.ToLowerInvariant()
}

function Invoke-StorageIoBenchmark {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RunLibrary,
        [Parameter(Mandatory = $true)]
        [string]$RunCache,
        [Parameter(Mandatory = $true)]
        [string]$RunOutput,
        [Parameter(Mandatory = $true)]
        [string]$RunLabel,
        [Parameter(Mandatory = $true)]
        [string]$RunProfile,
        [string]$RunNotes = ""
    )

    $resolvedLibrary = Resolve-Directory -Path $RunLibrary -Name "Library"
    $resolvedCache = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($RunCache)
    New-Item -ItemType Directory -Force -Path $resolvedCache | Out-Null

    $goArgs = @(
        "run", "./cmd/storageiobench",
        "-library", $resolvedLibrary,
        "-cache", $resolvedCache,
        "-out", $RunOutput,
        "-label", $RunLabel,
        "-profile", $RunProfile,
        "-notes", $RunNotes,
        "-max-files", "$MaxFiles",
        "-read-mb", "$ReadMB",
        "-write-files", "$WriteFiles",
        "-write-kb", "$WriteKB",
        "-cover-samples", "$CoverSamples",
        "-cover-read-kb", "$CoverReadKB",
        "-cover-write-kb", "$CoverWriteKB",
        "-cover-concurrency", "$CoverConcurrency",
        "-reader-probes", "$ReaderProbes",
        "-reader-kb", "$ReaderKB",
        "-background-readers", "$BackgroundReaders"
    )

    Write-Host "Running storage IO benchmark: $RunLabel"
    & go @goArgs
    if ($LASTEXITCODE -ne 0) {
        throw "storageiobench failed for $RunLabel with exit code $LASTEXITCODE"
    }
}

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$resolvedOutputDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath((Join-Path $repoRoot $OutputDir))
New-Item -ItemType Directory -Force -Path $resolvedOutputDir | Out-Null

$timestamp = Get-Date -Format "yyyy-MM-ddTHHmmss"
$primarySlug = New-Slug -Value $Label
$primaryOutput = Join-Path $resolvedOutputDir "$timestamp-$primarySlug-storage-io.md"

Push-Location $repoRoot
try {
    Invoke-StorageIoBenchmark `
        -RunLibrary $Library `
        -RunCache $Cache `
        -RunOutput $primaryOutput `
        -RunLabel $Label `
        -RunProfile $Profile `
        -RunNotes $Notes

    if (-not [string]::IsNullOrWhiteSpace($CompareLibrary)) {
        $compareCachePath = $CompareCache
        if ([string]::IsNullOrWhiteSpace($compareCachePath)) {
            $compareCachePath = $Cache
        }
        $compareSlug = New-Slug -Value $CompareLabel
        $compareOutput = Join-Path $resolvedOutputDir "$timestamp-$compareSlug-storage-io.md"
        Invoke-StorageIoBenchmark `
            -RunLibrary $CompareLibrary `
            -RunCache $compareCachePath `
            -RunOutput $compareOutput `
            -RunLabel $CompareLabel `
            -RunProfile $CompareProfile `
            -RunNotes $CompareNotes
    }
}
finally {
    Pop-Location
}

Write-Host "Storage IO baseline complete."
Write-Host "Output directory: $resolvedOutputDir"
