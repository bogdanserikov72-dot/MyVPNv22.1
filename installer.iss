; =============================================================================
; Intourist VPN — Inno Setup Script
; Версия: 2.2.0
;
; Устанавливает приложение как полноценную программу в Program Files.
; Структура после установки:
;   %ProgramFiles%\Intourist VPN\
;     intourist_vpn_gui.exe   <- PyInstaller onedir launcher
;     IntouristVPN_GUI\      <- все файлы PyInstaller onedir (включая веб-UI)
;     myvpn.exe               <- Go-бинарь (helper-режим)
;     wintun.dll         <- обязательно рядом с myvpn.exe
;     xray.exe           <- (опционально, для sub-режима)
;     geoip.dat
;     geosite.dat
;     config.json
;     bin\
;       helper.exe
;       tun2socks.exe
;       helper.config.yaml
;     logs\              <- создаётся при первом запуске
;     configs\
;     temp\
;     uninstall.exe      <- автоматически создаётся Inno Setup
; =============================================================================

#define MyAppName      "Intourist VPN"
#define MyAppVersion   "2.2.0"
#define MyAppPublisher "Intourist VPN"
#define MyAppExeName   "intourist_vpn_gui.exe"
#define MyAppURL       "https://t.me/blokirovki_ru"

[Setup]
; --- Идентификация ---
AppId={{8F3A2D1B-4E6C-4F7A-9B2E-1C3D5E7F9A0B}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}

; --- Директория установки ---
; autopf = Program Files или Program Files (x86) в зависимости от архитектуры
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes

; --- Внешний вид ---
SetupIconFile=icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
UninstallDisplayName={#MyAppName} {#MyAppVersion}

; --- Сжатие ---
Compression=lzma2/ultra64
SolidCompression=yes
LZMAUseSeparateProcess=yes

; --- Безопасность ---
; Приложение управляет WinTUN — требует прав администратора
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog

; --- Вывод ---
OutputDir=installer_dist
OutputBaseFilename=IntouristVPN_Setup_{#MyAppVersion}

; --- Архитектура ---
; Только x64 (wintun.dll — 64-битная)
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64

; --- Обновление ---
; При установке поверх существующей версии — корректно обновляем
CloseApplications=yes
CloseApplicationsFilter=*.exe
RestartApplications=no

; --- Прочее ---
ShowLanguageDialog=no
LanguageDetectionMethod=locale
WizardStyle=modern
DisableWelcomePage=no

[Languages]
Name: "russian";    MessagesFile: "compiler:Languages\Russian.isl"

[Messages]
BeveledLabel=Intourist VPN

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "quicklaunchicon"; Description: "{cm:CreateQuickLaunchIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked; OnlyBelowVersion: 6.1

[Dirs]
; Создаём рабочие директории сразу при установке
Name: "{app}\logs"
Name: "{app}\configs"
Name: "{app}\temp"
Name: "{app}\bin"

[Files]
; =============================================================================
; PyInstaller onedir output (dist\IntouristVPN_GUI\)
; Копируем ВСЮ папку, включая все Qt-библиотеки
; =============================================================================
Source: "dist\IntouristVPN_GUI\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

; =============================================================================
; Go-бинарь myvpn.exe (основной VPN-движок, helper-режим)
; =============================================================================
Source: "myvpn.exe"; DestDir: "{app}"; Flags: ignoreversion

; =============================================================================
; wintun.dll — КРИТИЧНО: должна лежать рядом с myvpn.exe и intourist_vpn_gui.exe
; =============================================================================
Source: "wintun.dll"; DestDir: "{app}"; Flags: ignoreversion

; =============================================================================
; bin\ — вспомогательные бинари
; =============================================================================
Source: "bin\helper.exe";            DestDir: "{app}\bin"; Flags: ignoreversion
Source: "bin\tun2socks.exe";         DestDir: "{app}\bin"; Flags: ignoreversion
Source: "bin\helper.config.yaml";    DestDir: "{app}\bin"; Flags: ignoreversion
; wintun.dll также нужна в bin\ (для tun2socks)
Source: "wintun.dll";                DestDir: "{app}\bin"; Flags: ignoreversion

; =============================================================================
; Xray и geo-файлы (для sub-режима, опционально)
; =============================================================================
Source: "bin\xray.exe";   DestDir: "{app}\bin"; Flags: ignoreversion skipifsourcedoesntexist
Source: "geoip.dat";      DestDir: "{app}";     Flags: ignoreversion skipifsourcedoesntexist
Source: "geosite.dat";    DestDir: "{app}";     Flags: ignoreversion skipifsourcedoesntexist

; =============================================================================
; config.json — дефолтная конфигурация
; =============================================================================
Source: "config.json"; DestDir: "{app}"; Flags: ignoreversion onlyifdoesntexist

; =============================================================================
; Иконка
; =============================================================================
Source: "icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; Меню «Пуск»
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\icon.ico"; Comment: "Intourist VPN Client"
Name: "{group}\Удалить {#MyAppName}"; Filename: "{uninstallexe}"

; Рабочий стол (по выбору пользователя)
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\icon.ico"; Tasks: desktopicon; Comment: "Intourist VPN Client"

; Панель быстрого запуска (Windows XP/Vista)
Name: "{userappdata}\Microsoft\Internet Explorer\Quick Launch\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: quicklaunchicon

[Run]
; Запустить приложение после установки (опционально)
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent runascurrentuser

[UninstallRun]
; Завершаем процессы перед удалением
Filename: "{sys}\taskkill.exe"; Parameters: "/f /im intourist_vpn_gui.exe"; Flags: runhidden skipifdoesntexist
Filename: "{sys}\taskkill.exe"; Parameters: "/f /im myvpn.exe";     Flags: runhidden skipifdoesntexist
Filename: "{sys}\taskkill.exe"; Parameters: "/f /im helper.exe";    Flags: runhidden skipifdoesntexist
Filename: "{sys}\taskkill.exe"; Parameters: "/f /im tun2socks.exe"; Flags: runhidden skipifdoesntexist
Filename: "{sys}\taskkill.exe"; Parameters: "/f /im xray.exe";      Flags: runhidden skipifdoesntexist

[UninstallDelete]
; Удаляем рабочие директории, созданные во время работы
Type: filesandordirs; Name: "{app}\logs"
Type: filesandordirs; Name: "{app}\temp"
; configs\ НЕ удаляем — там могут быть пользовательские настройки
; Если нужно полное удаление, раскомментировать:
; Type: filesandordirs; Name: "{app}\configs"

[Registry]
; Регистрируем приложение в «Установка и удаление программ»
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey
Root: HKLM; Subkey: "Software\{#MyAppPublisher}\{#MyAppName}"; ValueType: string; ValueName: "Version"; ValueData: "{#MyAppVersion}"

[Code]
// Проверяем наличие предыдущей версии и предлагаем удалить её
function InitializeSetup(): Boolean;
var
  UninstPath: string;
  UninstallString: string;
  ResultCode: Integer; // 1. Объявляем переменную для кода ответа
begin
  Result := True;
  
  // Проверяем наличие предыдущей установки
  UninstPath := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{8F3A2D1B-4E6C-4F7A-9B2E-1C3D5E7F9A0B}_is1';
  if RegQueryStringValue(HKLM, UninstPath, 'UninstallString', UninstallString) then
  begin
    if MsgBox('Обнаружена предыдущая версия Intourist VPN. Удалить её перед установкой?',
              mbConfirmation, MB_YESNO) = IDYES then
    begin
      // 2. Передаем ResultCode вместо 0 последним параметром
      Exec(RemoveQuotes(UninstallString), '/SILENT /NORESTART', '', SW_SHOW, ewWaitUntilTerminated, ResultCode);
    end;
  end;
end;