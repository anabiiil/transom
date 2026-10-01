# Build native Windows GUI and console executables and portable ZIP packages.
# Example: powershell -ExecutionPolicy Bypass -File windows\build.ps1 -Version 0.2.0
[CmdletBinding()]
param(
    [string]$Version = $(if ($env:VERSION) { $env:VERSION } else { "0.2.0" }),
    [ValidateSet("amd64", "arm64")]
    [string[]]$Architectures = @("amd64", "arm64"),
    [string]$OutputDirectory = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $RepoRoot "dist" }
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$OriginalGOOS = $env:GOOS
$OriginalGOARCH = $env:GOARCH
$OriginalCGO = $env:CGO_ENABLED

Push-Location $RepoRoot
try {
    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
    # Resource compilation must run on the build host, before cross-compiling.
    $env:GOOS = & go env GOHOSTOS
    $env:GOARCH = & go env GOHOSTARCH
    & go run github.com/tc-hib/go-winres@v0.3.3 make `
        --in windows/winres.json --out rsrc --arch amd64,arm64 `
        --file-version $Version --product-version $Version
    if ($LASTEXITCODE -ne 0) { throw "Windows resource compilation failed." }
    Copy-Item "rsrc_windows_amd64.syso", "rsrc_windows_arm64.syso" (Join-Path $PSScriptRoot "setup") -Force
    $PayloadDirectory = Join-Path $PSScriptRoot "setup\payload"
    New-Item -ItemType Directory -Path $PayloadDirectory -Force | Out-Null

    $env:GOOS = "windows"
    $env:CGO_ENABLED = "0"
    $CommonFlags = "-s -w -X transom/internal/version.Number=$Version"
    foreach ($Architecture in $Architectures) {
        $env:GOARCH = $Architecture
        $PackageDirectory = Join-Path $OutputDirectory "windows-$Architecture"
        New-Item -ItemType Directory -Path $PackageDirectory -Force | Out-Null
        Write-Host "Building Transom $Version for Windows/$Architecture..."
        & go build -trimpath `
            -ldflags "$CommonFlags -H=windowsgui -X transom/cmd.guiBuild=true" `
            -o (Join-Path $PackageDirectory "Transom.exe") .
        if ($LASTEXITCODE -ne 0) { throw "Windows desktop build failed for $Architecture." }
        & go build -trimpath -ldflags $CommonFlags `
            -o (Join-Path $PackageDirectory "transom-cli.exe") .
        if ($LASTEXITCODE -ne 0) { throw "Windows console build failed for $Architecture." }
        $WindowsReadme = Join-Path $PSScriptRoot "README.md"
        if (Test-Path $WindowsReadme) {
            Copy-Item $WindowsReadme (Join-Path $PackageDirectory "README.md") -Force
        } else {
            Copy-Item "README.md" (Join-Path $PackageDirectory "README.md") -Force
        }
        Copy-Item "LICENSE" (Join-Path $PackageDirectory "LICENSE") -Force
        Copy-Item (Join-Path $PSScriptRoot "THIRD_PARTY_NOTICES.txt") (Join-Path $PackageDirectory "THIRD_PARTY_NOTICES.txt") -Force
        foreach ($TextName in @("README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt")) {
            Copy-Item (Join-Path $PackageDirectory $TextName) (Join-Path $PayloadDirectory $TextName) -Force
        }
        & go build -trimpath -tags uninstaller -ldflags "$CommonFlags -H=windowsgui" `
            -o (Join-Path $PayloadDirectory "Uninstall.exe") ./windows/setup
        if ($LASTEXITCODE -ne 0) { throw "Windows uninstaller build failed for $Architecture." }
        Copy-Item (Join-Path $PackageDirectory "Transom.exe") (Join-Path $PayloadDirectory "Transom.exe") -Force
        Copy-Item (Join-Path $PackageDirectory "transom-cli.exe") (Join-Path $PayloadDirectory "transom-cli.exe") -Force
        $SetupExecutable = Join-Path $OutputDirectory "Transom-${Version}-Setup-${Architecture}.exe"
        & go build -trimpath -ldflags "$CommonFlags -H=windowsgui" `
            -o $SetupExecutable ./windows/setup
        if ($LASTEXITCODE -ne 0) { throw "Windows installer build failed for $Architecture." }

        $Archive = Join-Path $OutputDirectory "transom_${Version}_windows_${Architecture}.zip"
        Compress-Archive -Path (Join-Path $PackageDirectory "*") -DestinationPath $Archive -Force
        Write-Host "Built $(Join-Path $PackageDirectory 'Transom.exe')"
        Write-Host "Built $SetupExecutable"
        Write-Host "Packaged $Archive"
    }
} finally {
    foreach ($PayloadName in @("Transom.exe", "transom-cli.exe", "Uninstall.exe", "README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt")) {
        $PayloadFile = Join-Path $PSScriptRoot "setup\payload\$PayloadName"
        if (Test-Path $PayloadFile) { Remove-Item $PayloadFile -Force }
    }
    $env:GOOS = $OriginalGOOS
    $env:GOARCH = $OriginalGOARCH
    $env:CGO_ENABLED = $OriginalCGO
    Pop-Location
}
