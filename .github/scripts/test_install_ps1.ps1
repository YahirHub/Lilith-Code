$ErrorActionPreference = 'Stop'
$installer = Join-Path (Split-Path $PSScriptRoot -Parent) '..\install.ps1'
$installer = [System.IO.Path]::GetFullPath($installer)

function Assert-InstallerArchitecture {
    param(
        [Parameter(Mandatory = $true)][string]$InputArchitecture,
        [Parameter(Mandatory = $true)][string]$ExpectedAsset
    )
    $previousTest = $env:LI_INSTALLER_TEST_ONLY
    $previousArch = $env:LI_ARCH
    try {
        $env:LI_INSTALLER_TEST_ONLY = '1'
        $env:LI_ARCH = $InputArchitecture
        $output = @(& powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File $installer)
        if ($LASTEXITCODE -ne 0) { throw "install.ps1 terminó con código $LASTEXITCODE" }
        if ($output -notcontains "asset=$ExpectedAsset") {
            throw "Arquitectura $InputArchitecture no seleccionó $ExpectedAsset. Salida: $($output -join '; ')"
        }
    } finally {
        $env:LI_INSTALLER_TEST_ONLY = $previousTest
        $env:LI_ARCH = $previousArch
    }
}

Assert-InstallerArchitecture -InputArchitecture 'AMD64' -ExpectedAsset 'li-windows-amd64.exe'
Assert-InstallerArchitecture -InputArchitecture 'ARM64' -ExpectedAsset 'li-windows-arm64.exe'
Write-Host 'PowerShell installer architecture tests passed.'
