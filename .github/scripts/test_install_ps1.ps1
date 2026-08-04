$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$installer = Join-Path $repositoryRoot 'install.ps1'
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
        $output = @(& $installer)
        if (-not $?) { throw 'install.ps1 no terminó correctamente' }
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
Write-Host 'PowerShell installer architecture tests passed on the current host.'
