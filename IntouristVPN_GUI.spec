# IntouristVPN_GUI.spec  —  PyInstaller 6.x
# ВАЖНО: onedir режим (НЕ onefile) — единственно правильный вариант.
#   1) wintun.dll должна лежать физически рядом с exe (onefile распаковывает
#      её во временную TEMP-папку — LoadLibrary иногда не находит адаптер).
#   2) QtWebEngine (используется новым интерфейсом) требует, чтобы
#      QtWebEngineProcess.exe и его ресурсы (locales/, resources/) лежали
#      рядом с главным exe — при onefile это тоже ненадёжно.
#
# Запуск:  pyinstaller IntouristVPN_GUI.spec
#
# Переменные окружения:
#   INTOURIST_VERSION  — версия (по умолчанию "2.2.0")
#   INTOURIST_BIN_DIR  — путь к bin\  (по умолчанию "./bin")
#   INTOURIST_EXE      — путь к myvpn.exe (по умолчанию "./myvpn.exe")
#   INTOURIST_ICON     — путь к .ico (по умолчанию "./icon.ico")

import os
from pathlib import Path

ROOT = Path(SPECPATH)

version   = os.environ.get("INTOURIST_VERSION", "2.2.0")
bin_dir   = Path(os.environ.get("INTOURIST_BIN_DIR", str(ROOT / "bin")))
myvpn_exe = Path(os.environ.get("INTOURIST_EXE",     str(ROOT / "myvpn.exe")))
icon_path = Path(os.environ.get("INTOURIST_ICON",    str(ROOT / "icon.ico")))
cfg_gen   = ROOT / "config_gen.py"
manifest  = ROOT / "myvpn_gui.manifest"
web_ui    = ROOT / "intourist_vps_premium_ui"

# ── Данные, встраиваемые в onedir ──────────────────────────────────────────
datas    = []
binaries = []

if bin_dir.exists():
    datas.append((str(bin_dir), "bin"))

if myvpn_exe.exists():
    datas.append((str(myvpn_exe), "."))

if cfg_gen.exists():
    datas.append((str(cfg_gen), "."))

# Веб-интерфейс (HTML/CSS/иконки) — кладём в подпапку рядом с exe.
# ВАЖНО: используем явное перечисление, чтобы PyInstaller не пропустил.
if web_ui.exists():
    # Копируем всю папку целиком
    datas.append((str(web_ui), "intourist_vps_premium_ui"))
    # На случай, если PyInstaller её не подхватит — копируем явно основные файлы
    index_html = web_ui / "index.html"
    style_css = web_ui / "style.css"
    if index_html.exists():
        datas.append((str(index_html), "intourist_vps_premium_ui"))
    if style_css.exists():
        datas.append((str(style_css), "intourist_vps_premium_ui"))
    images_dir = web_ui / "images"
    if images_dir.exists():
        datas.append((str(images_dir), "intourist_vps_premium_ui/images"))

wintun_dll = ROOT / "wintun.dll"
if wintun_dll.exists():
    binaries.append((str(wintun_dll), "."))
else:
    wintun_in_bin = bin_dir / "wintun.dll"
    if wintun_in_bin.exists():
        binaries.append((str(wintun_in_bin), "."))

for geo in ("geoip.dat", "geosite.dat"):
    p = ROOT / geo
    if p.exists():
        datas.append((str(p), "."))

config_json = ROOT / "config.json"
if config_json.exists():
    datas.append((str(config_json), "."))

# Иконка нужна не только PyInstaller'у (для ресурса .exe), но и самому
# приложению в рантайме — оно ставит её как window/taskbar icon.
if icon_path.exists():
    datas.append((str(icon_path), "."))

# ── Analysis ──────────────────────────────────────────────────────────────
a = Analysis(
    [str(ROOT / "myvpn_gui.py")],
    pathex=[str(ROOT)],
    binaries=binaries,
    datas=datas,
    hiddenimports=[
        "PyQt6.QtCore",
        "PyQt6.QtGui",
        "PyQt6.QtWidgets",
        "PyQt6.QtWebEngineWidgets",
        "PyQt6.QtWebEngineCore",
        "PyQt6.QtWebChannel",
        "requests",
        "winreg",
        "ctypes",
        "config_gen",
        "vpn_bridge_api",
    ],
    hookspath=[],
    runtime_hooks=[],
    excludes=["tkinter", "matplotlib", "numpy", "scipy", "PIL"],
    noarchive=False,
)

pyz = PYZ(a.pure, a.zipped_data)

# UPX ломает Qt6WebEngineCore.dll / QtWebEngineProcess.exe / Qt6*.dll —
# исключаем всё, что относится к Qt и WebEngine, из сжатия, иначе собранный
# .exe либо не запустится, либо будет падать без вменяемой ошибки.
_UPX_EXCLUDE = [
    "wintun.dll", "myvpn.exe", "helper.exe", "tun2socks.exe", "xray.exe",
    "Qt6*.dll", "QtWebEngineProcess.exe", "d3dcompiler_47.dll",
    "libEGL.dll", "libGLESv2.dll", "opengl32sw.dll",
]

# ── ONEDIR exe (НЕ onefile!) ───────────────────────────────────────────────
exe = EXE(
    pyz,
    a.scripts,
    [],                      # <-- пусто при onedir
    exclude_binaries=True,   # <-- True при onedir
    name="intourist_vpn_gui",
    debug=False,
    strip=False,
    upx=True,
    upx_exclude=_UPX_EXCLUDE,
    console=False,
    manifest=str(manifest) if manifest.exists() else None,
    icon=str(icon_path) if icon_path.exists() else None,
    uac_admin=True,
)

# ── COLLECT — собирает все файлы в dist\IntouristVPN_GUI\ ──────────────────
coll = COLLECT(
    exe,
    a.binaries,
    a.zipfiles,
    a.datas,
    strip=False,
    upx=True,
    upx_exclude=_UPX_EXCLUDE,
    name="IntouristVPN_GUI",
)
