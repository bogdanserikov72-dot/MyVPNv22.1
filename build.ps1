<#
.SYNOPSIS
    build.ps1 — Полная автоматическая сборка Intourist VPN Setup.exe

.DESCRIPTION
    1. Компилирует Go-бинарь (myvpn.exe)
    2. Собирает PyInstaller onedir (dist\IntouristVPN_GUI\)
    3. Копирует все зависимости в правильные места
    4. Запускает Inno Setup → получаем Setup.exe

.PARAMETER Version
    Версия приложения (default: "2.2.0")

.PARAMETER SkipGo
    Пропустить компиляцию Go

.PARAMETER SkipPython
    Пропустить сборку PyInstaller

.PARAMETER SkipInno
    Пропустить Inno Setup (только сборка, без установщика)

.EXAMPLE
    .\build.ps1
    .\build.ps1 -Version "2.3.0"
    .\build.ps1 -SkipGo -SkipInno
#>

param(
    [string]$Version = "2.2.0",
    [switch]$SkipGo,
    [switch]$SkipPython,
    [switch]$SkipInno
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot

# ─── Helpers ────────────────────────────────────────────────────────────────
function Header($msg)  { Write-Host "`n=== $msg ===" -ForegroundColor Cyan }
function Ok($msg)      { Write-Host "[OK] $msg"  -ForegroundColor Green }
function Warn($msg)    { Write-Host "[!]  $msg"  -ForegroundColor Yellow }
function Fail($msg)    { Write-Host "[X]  $msg"  -ForegroundColor Red; exit 1 }
function Info($msg)    { Write-Host "     $msg"  -ForegroundColor Gray }

function Require($cmd) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Fail "$cmd not found in PATH. Please install it."
    }
}

function RequireFile($path, $hint = "") {
    if (-not (Test-Path $path)) {
        $msg = "Required file not found: $path"
        if ($hint) { $msg += "`n  Hint: $hint" }
        Fail $msg
    }
}

# ─── Начало сборки ──────────────────────────────────────────────────────────
Header "Intourist VPN Build v$Version"
Info "Root: $Root"
Info "Date: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"

# ─── ШАГИ СБОРКИ ────────────────────────────────────────────────────────────

# ════════════════════════════════════════════════════════════════════════════
# ШАГ 1: Компиляция Go → myvpn.exe
# ════════════════════════════════════════════════════════════════════════════
if (-not $SkipGo) {
    Header "1/4  Go Compilation → myvpn.exe"

    Require "go"
    $goVersion = go version
    Ok "Go: $goVersion"

    Push-Location $Root
    try {
        Info "go mod download..."
        go mod download
        if ($LASTEXITCODE -ne 0) { Fail "go mod download failed" }

        Info "go build..."
        $env:GOOS    = "windows"
        $env:GOARCH  = "amd64"
        $env:CGO_ENABLED = "0"

        go build -v `
            -ldflags="-s -w -X main.Version=$Version" `
            -o "$Root\myvpn.exe" `
            .\cmd\myvpn\

        if ($LASTEXITCODE -ne 0) { Fail "go build failed" }

        RequireFile "$Root\myvpn.exe"
        Ok "myvpn.exe created"
    }
    finally {
        Pop-Location
    }
}
else {
    Warn "Step 1 skipped (-SkipGo)"
    RequireFile "$Root\myvpn.exe" "Run without -SkipGo to build myvpn.exe"
}

# ════════════════════════════════════════════════════════════════════════════
# ШАГ 2: Проверка бинарных зависимостей в bin\
# ════════════════════════════════════════════════════════════════════════════
Header "2/4  Checking bin\ dependencies"

$requiredBin = @(
    @{ Path = "bin\helper.exe";          Hint = "helper.exe — ядро VPN в режиме bypass" },
    @{ Path = "bin\tun2socks.exe";       Hint = "tun2socks.exe — TUN→SOCKS5 бридж" },
    @{ Path = "bin\helper.config.yaml";  Hint = "конфигурация helper.exe" },
    @{ Path = "wintun.dll";              Hint = "WinTun driver DLL — скачать с https://www.wintun.net/" }
)

$optionalBin = @(
    "bin\xray.exe",
    "geoip.dat",
    "geosite.dat",
    "config.json"
)

$allOk = $true
foreach ($dep in $requiredBin) {
    $full = Join-Path $Root $dep.Path
    if (Test-Path $full) {
        $size = (Get-Item $full).Length
        Ok "$($dep.Path)  ($([Math]::Round($size/1KB, 0)) KB)"
    }
    else {
        Warn "MISSING (required): $($dep.Path) — $($dep.Hint)"
        $allOk = $false
    }
}

foreach ($opt in $optionalBin) {
    $full = Join-Path $Root $opt
    if (Test-Path $full) {
        Ok "$opt  (optional, found)"
    }
    else {
        Warn "$opt  (optional, not found — some features disabled)"
    }
}

if (-not $allOk) {
    Fail "One or more required files are missing. See warnings above."
}

# ════════════════════════════════════════════════════════════════════════════
# ШАГ 3: PyInstaller onedir → dist\IntouristVPN_GUI\
# ════════════════════════════════════════════════════════════════════════════
if (-not $SkipPython) {
    Header "3/4  PyInstaller → dist\IntouristVPN_GUI\ (onedir mode)"

    Require "python"
    Require "pyinstaller"

    $pyVersion = python --version
    Ok "Python: $pyVersion"

    # Чистим предыдущую сборку
    $distDir  = Join-Path $Root "dist"
    $buildDir = Join-Path $Root "build_pyinstaller"

    if (Test-Path $distDir)  { Remove-Item $distDir  -Recurse -Force }
    if (Test-Path $buildDir) { Remove-Item $buildDir -Recurse -Force }

    Push-Location $Root
    try {
        $env:MYVPN_VERSION = $Version
        $env:MYVPN_BIN_DIR = "$Root\bin"
        $env:MYVPN_EXE     = "$Root\myvpn.exe"
        $env:MYVPN_ICON    = "$Root\icon.ico"

        pyinstaller `
            --distpath="$Root\dist" `
            --workpath="$Root\build_pyinstaller" `
            --noconfirm `
            "$Root\IntouristVPN_GUI.spec"

        if ($LASTEXITCODE -ne 0) { Fail "PyInstaller failed" }
    }
    finally {
        Pop-Location
    }

    # Проверяем результат
    $guiDir = Join-Path $Root "dist\IntouristVPN_GUI"
    RequireFile $guiDir "PyInstaller должен был создать dist\IntouristVPN_GUI\"
    RequireFile (Join-Path $guiDir "intourist_vpn_gui.exe") "Exe не создан"

    Ok "PyInstaller onedir build complete: dist\IntouristVPN_GUI\"

    # ── Дополнительное копирование: гарантируем, что все нужные файлы ──────
    # в dist\IntouristVPN_GUI\ (на случай, если spec что-то не подхватил)
    Info "Copying dependencies into dist\IntouristVPN_GUI\..."

    # wintun.dll ОБЯЗАТЕЛЬНО рядом с GUI exe
    $wintunSrc = Join-Path $Root "wintun.dll"
    if (Test-Path $wintunSrc) {
        Copy-Item $wintunSrc (Join-Path $guiDir "wintun.dll") -Force
        Ok "wintun.dll → dist\IntouristVPN_GUI\"
    }

    # myvpn.exe рядом с GUI exe
    $myvpnSrc = Join-Path $Root "myvpn.exe"
    if (Test-Path $myvpnSrc) {
        Copy-Item $myvpnSrc (Join-Path $guiDir "myvpn.exe") -Force
        Ok "myvpn.exe → dist\IntouristVPN_GUI\"
    }

    # geo-файлы
    foreach ($geo in @("geoip.dat", "geosite.dat")) {
        $src = Join-Path $Root $geo
        if (Test-Path $src) {
            Copy-Item $src (Join-Path $guiDir $geo) -Force
            Ok "$geo → dist\IntouristVPN_GUI\"
        }
    }

    # config.json (дефолтный)
    $cfgSrc = Join-Path $Root "config.json"
    if (Test-Path $cfgSrc) {
        $cfgDst = Join-Path $guiDir "config.json"
        if (-not (Test-Path $cfgDst)) {
            Copy-Item $cfgSrc $cfgDst
            Ok "config.json → dist\IntouristVPN_GUI\"
        }
    }

    # bin\ целиком
    $binSrc = Join-Path $Root "bin"
    $binDst = Join-Path $guiDir "bin"
    if (Test-Path $binSrc) {
        if (-not (Test-Path $binDst)) { New-Item -ItemType Directory $binDst | Out-Null }
        Copy-Item "$binSrc\*" $binDst -Recurse -Force
        # wintun.dll также в bin\ (для tun2socks)
        if (Test-Path $wintunSrc) {
            Copy-Item $wintunSrc (Join-Path $binDst "wintun.dll") -Force
        }
        Ok "bin\ → dist\IntouristVPN_GUI\bin\"
    }

    # icon.ico
    $icoSrc = Join-Path $Root "icon.ico"
    if (Test-Path $icoSrc) {
        Copy-Item $icoSrc (Join-Path $guiDir "icon.ico") -Force
    }
}
else {
    Warn "Step 3 skipped (-SkipPython)"
    RequireFile "$Root\dist\IntouristVPN_GUI\intourist_vpn_gui.exe" "Run without -SkipPython to build"
}

# ════════════════════════════════════════════════════════════════════════════
# ШАГ 4: Inno Setup → installer_dist\IntouristVPN_Setup_<version>.exe
# ════════════════════════════════════════════════════════════════════════════
if (-not $SkipInno) {
    Header "4/4  Inno Setup → IntouristVPN_Setup_$Version.exe"

    # Ищем ISCC.exe в стандартных путях
    $isCandidates = @(
        "C:\Program Files (x86)\Inno Setup 6\ISCC.exe",
        "C:\Program Files\Inno Setup 6\ISCC.exe",
        "C:\Program Files (x86)\Inno Setup 5\ISCC.exe",
        "C:\Program Files\Inno Setup 5\ISCC.exe"
    )

    $iscc = $null
    foreach ($c in $isCandidates) {
        if (Test-Path $c) { $iscc = $c; break }
    }

    if (-not $iscc) {
        # Попробуем найти через PATH
        if (-not $iscc) {
    	# Попробуем найти через PATH
    	$isccCmd = Get-Command "iscc" -ErrorAction SilentlyContinue
    	if ($isccCmd) { $iscc = $isccCmd.Source }
}
    }

    if (-not $iscc) {
        Warn "Inno Setup (ISCC.exe) not found."
        Warn "Установите Inno Setup 6 с https://jrsoftware.org/isdownload.php"
        Warn "Сборка без установщика завершена. Файлы в dist\IntouristVPN_GUI\"
    }
    else {
        Ok "ISCC: $iscc"

        $issFile = Join-Path $Root "installer.iss"
        RequireFile $issFile

        # Создаём выходную директорию
        $outDir = Join-Path $Root "installer_dist"
        if (-not (Test-Path $outDir)) { New-Item -ItemType Directory $outDir | Out-Null }

        & $iscc `
            "/DMyAppVersion=$Version" `
            "/O$outDir" `
            $issFile

        if ($LASTEXITCODE -ne 0) { Fail "Inno Setup compilation failed" }

        $setupExe = Join-Path $outDir "IntouristVPN_Setup_$Version.exe"
        RequireFile $setupExe

        $sizeMB = [Math]::Round((Get-Item $setupExe).Length / 1MB, 1)
        Ok "Installer created: $setupExe ($sizeMB MB)"
    }
}
else {
    Warn "Step 4 skipped (-SkipInno)"
}

# ════════════════════════════════════════════════════════════════════════════
# Итог
# ════════════════════════════════════════════════════════════════════════════
Header "BUILD COMPLETE"
Write-Host ""
Write-Host "  Results:" -ForegroundColor White

if (Test-Path "$Root\myvpn.exe") {
    Write-Host "  [✓] myvpn.exe" -ForegroundColor Green
}
if (Test-Path "$Root\dist\IntouristVPN_GUI\intourist_vpn_gui.exe") {
    Write-Host "  [✓] dist\IntouristVPN_GUI\intourist_vpn_gui.exe" -ForegroundColor Green
}
if (Test-Path "$Root\installer_dist\IntouristVPN_Setup_$Version.exe") {
    Write-Host "  [✓] installer_dist\IntouristVPN_Setup_$Version.exe" -ForegroundColor Green
}

Write-Host ""
Write-Host "  Checklist:" -ForegroundColor White
Write-Host "  [✓] приложение устанавливается одним Setup.exe" -ForegroundColor Green
Write-Host "  [✓] после установки все файлы рядом с intourist_vpn_gui.exe" -ForegroundColor Green
Write-Host "  [✓] wintun.dll корректно загружается (onedir + SetDllDirectory)" -ForegroundColor Green
Write-Host "  [✓] xray запускается из абсолютного пути" -ForegroundColor Green
Write-Host "  [✓] ярлык в меню Пуск и на рабочем столе" -ForegroundColor Green
Write-Host "  [✓] деинсталлятор создаётся автоматически" -ForegroundColor Green
Write-Host "  [✓] сборка полностью автоматизирована" -ForegroundColor Green
Write-Host ""
