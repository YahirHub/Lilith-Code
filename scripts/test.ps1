[CmdletBinding()]
param(
    [switch]$Vet
)

$ErrorActionPreference = "Stop"
$repositoryRoot = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $repositoryRoot

function Test-DnsHost {
    param([Parameter(Mandatory = $true)][string]$HostName)

    try {
        [void][System.Net.Dns]::GetHostAddresses($HostName)
        return $true
    }
    catch {
        return $false
    }
}

function Invoke-Go {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "El comando 'go $($Arguments -join ' ')' terminó con código $LASTEXITCODE."
    }
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go no está disponible en PATH. Instala Go 1.25.12 o posterior y abre una terminal nueva."
}

$previousSumDB = $env:GOSUMDB
try {
    $sumDBOutput = & go env GOSUMDB
    if ($LASTEXITCODE -ne 0) {
        throw "No se pudo leer GOSUMDB mediante 'go env'."
    }
    $configuredSumDB = ($sumDBOutput | Out-String).Trim()

    if ($configuredSumDB -eq "off") {
        throw "GOSUMDB está desactivado. Restáuralo con: go env -u GOSUMDB"
    }

    if (($configuredSumDB -eq "" -or $configuredSumDB -eq "sum.golang.org") -and
        -not (Test-DnsHost "sum.golang.org")) {
        if (Test-DnsHost "sum.golang.google.cn") {
            $env:GOSUMDB = "sum.golang.google.cn"
            Write-Host "sum.golang.org no resolvió; se usará temporalmente el alias reconocido sum.golang.google.cn." -ForegroundColor Yellow
        }
        else {
            Write-Warning "No se pudo resolver sum.golang.org ni su alias. Se continuará con go.sum; si Go necesita un checksum nuevo, corrige DNS o conexión."
        }
    }

    Write-Host "Validando go.mod y go.sum..." -ForegroundColor Cyan
    Invoke-Go -Arguments @("mod", "tidy", "-diff")

    Write-Host "Ejecutando tests unitarios..." -ForegroundColor Cyan
    Invoke-Go -Arguments @("test", "-mod=readonly", "-tags=grammar_set_core", "-count=1", "-timeout=15m", "./...")

    if ($Vet) {
        Write-Host "Ejecutando go vet..." -ForegroundColor Cyan
        Invoke-Go -Arguments @("vet", "-mod=readonly", "-tags=grammar_set_core", "./...")
    }

    Write-Host "Verificando módulos descargados..." -ForegroundColor Cyan
    Invoke-Go -Arguments @("mod", "verify")

    Write-Host "Validación completada correctamente." -ForegroundColor Green
}
finally {
    $env:GOSUMDB = $previousSumDB
}
