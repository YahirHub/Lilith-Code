$ErrorActionPreference = 'Stop'

$Repository = if (-not [string]::IsNullOrWhiteSpace($env:LI_REPOSITORY)) { $env:LI_REPOSITORY.Trim() } else { 'YahirHub/Lilith-Code' }
$RequestedVersion = 'latest'
if (-not [string]::IsNullOrWhiteSpace($env:LI_VERSION)) {
    $RequestedVersion = $env:LI_VERSION.Trim()
} elseif ($null -ne $args -and @($args).Count -gt 0 -and -not [string]::IsNullOrWhiteSpace([string]$args[0])) {
    $RequestedVersion = ([string]$args[0]).Trim()
}

function Get-LilithArchitecture {
    if (-not [string]::IsNullOrWhiteSpace($env:LI_ARCH)) {
        $value = $env:LI_ARCH
    } else {
        $value = $null
        try {
            $runtimeArchitecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
            if ($null -ne $runtimeArchitecture) {
                $value = $runtimeArchitecture.ToString()
            }
        } catch {
            # Windows PowerShell 5.1 on older .NET Framework installations may
            # not expose RuntimeInformation.OSArchitecture reliably.
        }
        if ([string]::IsNullOrWhiteSpace([string]$value)) {
            $value = if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
                $env:PROCESSOR_ARCHITEW6432
            } else {
                $env:PROCESSOR_ARCHITECTURE
            }
        }
    }

    switch (([string]$value).Trim().ToLowerInvariant()) {
        { $_ -in @('x64', 'amd64', 'x86_64') } { return 'amd64' }
        { $_ -in @('arm64', 'aarch64') } { return 'arm64' }
        default { throw "Arquitectura no soportada: $value" }
    }
}

function Test-PathContains {
    param(
        [AllowNull()][string]$PathValue,
        [Parameter(Mandatory = $true)][string]$Directory
    )
    if ([string]::IsNullOrWhiteSpace($PathValue)) { return $false }
    $normalizedDirectory = $Directory.TrimEnd([char]'\')
    foreach ($entry in ($PathValue -split ';')) {
        if ([string]::IsNullOrWhiteSpace($entry)) { continue }
        if ($entry.Trim().TrimEnd([char]'\') -ieq $normalizedDirectory) { return $true }
    }
    return $false
}

$architecture = Get-LilithArchitecture
$asset = switch ($architecture) {
    'amd64' { 'li-windows-amd64.exe' }
    'arm64' { 'li-windows-arm64.exe' }
    default { throw "Arquitectura no soportada: $architecture" }
}

# Used by the Windows workflow to exercise the same PowerShell 5.1 code path
# without downloading or installing anything.
if ($env:LI_INSTALLER_TEST_ONLY -eq '1') {
    Write-Output "architecture=$architecture"
    Write-Output "asset=$asset"
    return
}

if ($RequestedVersion -eq 'latest') {
    $base = "https://github.com/$Repository/releases/latest/download"
    $displayVersion = 'la versión más reciente'
} else {
    $tag = if ($RequestedVersion.StartsWith('v')) { $RequestedVersion } else { "v$RequestedVersion" }
    $base = "https://github.com/$Repository/releases/download/$tag"
    $displayVersion = $tag
}

$localAppData = if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
    $env:LOCALAPPDATA
} else {
    [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
}
if ([string]::IsNullOrWhiteSpace($localAppData)) {
    throw 'No se pudo determinar LOCALAPPDATA para instalar Lilith.'
}

$installDir = if (-not [string]::IsNullOrWhiteSpace($env:LI_INSTALL_DIR)) {
    $env:LI_INSTALL_DIR
} else {
    Join-Path $localAppData 'Programs\Lilith\bin'
}
$target = Join-Path $installDir 'li.exe'
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("lilith-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    $download = Join-Path $tempDir $asset
    $checksums = Join-Path $tempDir 'SHA256SUMS.txt'
    Write-Host "Descargando $displayVersion para $architecture..."
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $download
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS.txt" -OutFile $checksums

    $escapedAsset = [regex]::Escape($asset)
    $line = Get-Content $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+\*?$escapedAsset$" } | Select-Object -First 1
    if (-not $line) { throw "El release no contiene el checksum de $asset" }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $download).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw 'El checksum SHA-256 no coincide' }

    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    $newTarget = "$target.new"
    Copy-Item -Force $download $newTarget
    Move-Item -Force $newTarget $target

    $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (Test-PathContains -PathValue $userPath -Directory $installDir)) {
        $newUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $installDir } else { "$installDir;$userPath" }
        [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    }

    $sessionPath = [string]$env:Path
    if (-not (Test-PathContains -PathValue $sessionPath -Directory $installDir)) {
        $env:Path = if ([string]::IsNullOrWhiteSpace($sessionPath)) { $installDir } else { "$installDir;$sessionPath" }
    }

    Write-Host "Lilith quedó instalado en $target"
    & $target version
    Write-Host 'Ejecuta: li'
} finally {
    Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
}
