$ErrorActionPreference = 'Stop'

$Repository = if ($env:LI_REPOSITORY) { $env:LI_REPOSITORY } else { 'YahirHub/Lilith-Code' }
$RequestedVersion = if ($env:LI_VERSION) { $env:LI_VERSION } elseif ($args.Count -gt 0) { $args[0] } else { 'latest' }

$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($arch) {
    'x64' { $asset = 'li-windows-amd64.exe' }
    'arm64' { $asset = 'li-windows-arm64.exe' }
    default { throw "Arquitectura no soportada: $arch" }
}

if ($RequestedVersion -eq 'latest') {
    $base = "https://github.com/$Repository/releases/latest/download"
    $displayVersion = 'la versión más reciente'
} else {
    $tag = if ($RequestedVersion.StartsWith('v')) { $RequestedVersion } else { "v$RequestedVersion" }
    $base = "https://github.com/$Repository/releases/download/$tag"
    $displayVersion = $tag
}

$installDir = Join-Path $env:LOCALAPPDATA 'Programs\Lilith\bin'
$target = Join-Path $installDir 'li.exe'
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("lilith-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    $download = Join-Path $tempDir $asset
    $checksums = Join-Path $tempDir 'SHA256SUMS.txt'
    Write-Host "Descargando $displayVersion para $arch..."
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $download
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS.txt" -OutFile $checksums

    $line = Get-Content $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+\*?$([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) { throw "El release no contiene el checksum de $asset" }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $download).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw 'El checksum SHA-256 no coincide' }

    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    $newTarget = "$target.new"
    Copy-Item -Force $download $newTarget
    Move-Item -Force $newTarget $target

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($userPath -split ';' | Where-Object { $_ })
    if (-not ($parts | Where-Object { $_.TrimEnd('\') -ieq $installDir.TrimEnd('\') })) {
        $newUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $installDir } else { "$installDir;$userPath" }
        [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    }
    if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $installDir.TrimEnd('\') })) {
        $env:Path = "$installDir;$env:Path"
    }

    Write-Host "Lilith quedó instalado en $target"
    & $target version
    Write-Host 'Ejecuta: li'
} finally {
    Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
}
