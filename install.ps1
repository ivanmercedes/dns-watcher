#Requires -Version 5.1
<#
.SYNOPSIS
    Instalador de DNS Watcher (Go) para Windows compatible con `irm <url> | iex`.
.DESCRIPTION
    Instala el binario `dns-watcher.exe` en %LOCALAPPDATA%\dns-watcher\bin,
    lo agrega al PATH del usuario, inicializa la línea base de DNS y opcionalmente
    crea una tarea programada en Windows.
#>
param(
    [string]$RepoUrl = $(if ($env:DNS_WATCHER_REPO) { $env:DNS_WATCHER_REPO } else { "https://github.com/ivanmercedes/dns-watcher" }),
    [string]$Branch = "main",
    [switch]$Schedule,
    [int]$IntervalMinutes = 15
)

$ErrorActionPreference = "Stop"

$AppDir = Join-Path $env:LOCALAPPDATA "dns-watcher"
$BinDir = Join-Path $AppDir "bin"
$ExePath = Join-Path $BinDir "dns-watcher.exe"

Write-Host "==> Instalando DNS Watcher (Go) en $AppDir" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $AppDir, $BinDir | Out-Null

$LocalSource = $PSScriptRoot
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }

if ($LocalSource -and (Test-Path (Join-Path $LocalSource "dist\dns-watcher-windows-$Arch.exe"))) {
    Write-Host "==> Copiando binario local dist\dns-watcher-windows-$Arch.exe" -ForegroundColor Gray
    Copy-Item -Force (Join-Path $LocalSource "dist\dns-watcher-windows-$Arch.exe") $ExePath
} elseif ($LocalSource -and (Test-Path (Join-Path $LocalSource "go.mod")) -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "==> Compilando desde código fuente local con Go..." -ForegroundColor Gray
    Push-Location $LocalSource
    go build -trimpath -ldflags="-s -w" -o $ExePath .
    Pop-Location
} else {
    if (-not $RepoUrl) {
        throw "Define `$env:DNS_WATCHER_REPO (ej. 'https://github.com/usuario/dns-watcher') o pasa -RepoUrl."
    }

    $ReleaseUrl = "$RepoUrl/releases/latest/download/dns-watcher-windows-$Arch.exe"
    $RawDistUrl = "$RepoUrl/raw/$Branch/dist/dns-watcher-windows-$Arch.exe"
    $Downloaded = $false

    foreach ($Url in @($ReleaseUrl, $RawDistUrl)) {
        try {
            Write-Host "==> Descargando binario desde $Url" -ForegroundColor Gray
            Invoke-WebRequest -Uri $Url -OutFile $ExePath -UseBasicParsing
            if ((Get-Item $ExePath).Length -gt 100000) {
                $Downloaded = $true
                break
            }
        } catch {}
    }

    if (-not $Downloaded) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw "No se encontró binario precompilado ni Go instalado. Sube dist/dns-watcher-windows-amd64.exe al repo o instala Go (winget install GoLang.Go)."
        }
        $ZipUrl = "$RepoUrl/archive/refs/heads/$Branch.zip"
        $TmpZip = Join-Path $env:TEMP "dns-watcher.zip"
        $TmpExtract = Join-Path $env:TEMP "dns-watcher-extract"
        if (Test-Path $TmpExtract) { Remove-Item -Recurse -Force $TmpExtract }
        Invoke-WebRequest -Uri $ZipUrl -OutFile $TmpZip -UseBasicParsing
        Expand-Archive -Path $TmpZip -DestinationPath $TmpExtract -Force
        $RootFolder = Get-ChildItem -Path $TmpExtract -Directory | Select-Object -First 1
        Push-Location $RootFolder.FullName
        go build -trimpath -ldflags="-s -w" -o $ExePath .
        Pop-Location
        Remove-Item -Force $TmpZip -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force $TmpExtract -ErrorAction SilentlyContinue
    }
}

# Agregar al PATH del usuario si no está presente
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not ($UserPath -split ";" | Where-Object { $_ -eq $BinDir })) {
    $NewPath = if ($UserPath) { "$UserPath;$BinDir" } else { $BinDir }
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    $env:Path = "$env:Path;$BinDir"
    Write-Host "==> Agregado $BinDir al PATH del usuario" -ForegroundColor Green
}

# Inicializar línea base si ya existe una lista de dominios
if (Test-Path (Join-Path $AppDir "domains.txt")) {
    & $ExePath init
} else {
    Write-Host "==> Importa tu lista de dominios con:" -ForegroundColor Yellow
    Write-Host "    dns-watcher domains import `"C:\ruta\a\Dominios.xlsx`"" -ForegroundColor Yellow
    Write-Host "    dns-watcher init" -ForegroundColor Yellow
}

if ($Schedule) {
    $TaskName = "DNS-Watcher"
    $Action = New-ScheduledTaskAction -Execute $ExePath -Argument "check"
    $Trigger = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes $IntervalMinutes)
    $Settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
    Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Settings $Settings -Force | Out-Null
    Write-Host "==> Tarea programada '$TaskName' creada (cada $IntervalMinutes min)." -ForegroundColor Green
}

Write-Host ""
Write-Host "Instalación completada. Comandos útiles:" -ForegroundColor Green
Write-Host "  dns-watcher status"
Write-Host "  dns-watcher check"
Write-Host "  dns-watcher watch --interval 300"
Write-Host "  dns-watcher config test-notify"
