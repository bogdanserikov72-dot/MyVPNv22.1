Intourist VPN — Windows VPN-клиент с двумя режимами подключения
Intourist VPN (внутреннее кодовое имя проекта — `MyVPN`) — это VPN-приложение для Windows с поддержкой полного туннелирования через TUN-адаптер (режим helper) и подписочной архитектуры с локальным SOCKS5-прокси (режим sub). Проект состоит из Go-бинарей (ядро VPN) и GUI на PyQt6 с веб-интерфейсом (HTML/CSS/JS), а также CLI-бинарника для консольного запуска.
Содержание
Особенности
Требования
Архитектура
Установка и сборка
Использование
CLI-режим
GUI-режим
Режимы подключения
Helper (полный туннель)
Sub (подписка / xray)
Разработка
Известные проблемы и решения
Лицензия
---
Особенности
✅ Два режима подключения:
Helper — полный туннель трафика через TUN-адаптер wintun; `helper.exe` поднимает соединение до облачного бридж-эндпоинта и открывает локальный SOCKS5 (127.0.0.1:1080), а `tun2socks.exe` заворачивает в него весь TUN-трафик
Sub — подписочная архитектура с локальным SOCKS5/HTTP-прокси через xray (VLESS, VMess, Trojan, Shadowsocks)
✅ Веб-интерфейс на PyQt6 WebEngine:
GUI теперь рендерится как HTML/CSS/JS-приложение (`intourist_vps_premium_ui/`) внутри `QWebEngineView`
Общение между веб-страницей и Python-бэкендом идёт через `QWebChannel` (мост `vpn_bridge_api.py`), все данные — в виде JSON-строк
Поддержка тёмной темы, пинга серверов, истории добавленных ссылок/подписок
✅ Двойной интерфейс:
Консольное приложение (`myvpn.exe`) для автоматизации и headless-запуска
Полноценный GUI (`intourist_vpn_gui.exe`) с автоматическим запросом прав администратора (UAC)
✅ Защита от системных сбоев Windows:
Автоматическая чистка осиротевших TUN-адаптеров и bypass-маршрутов при запуске/остановке
Безопасное отключение системного прокси при завершении
Широковещательное уведомление браузерам об изменении прокси (WM_SETTINGCHANGE)
Глобальный перехват необработанных исключений (`sys.excepthook`) — GUI пишет краш-лог вместо падения всего процесса
✅ Гибкий парсинг серверов:
Разбор одиночных ссылок (`vless://`, `vmess://`, `ss://`, `trojan://`) и base64-подписок
Автоматическое определение страны сервера по названию/хосту (для отображения флага)
---
Требования
Обязательно
OS: Windows 10 / Windows 11 (64-bit)
Права: Администратор (GUI сам запрашивает повышение через UAC при старте)
Сетевой адаптер: Интернет-соединение
Для сборки Go-части (`myvpn.exe`)
Go 1.20+ (golang.org)
PowerShell 5.0+
Git (для git-зависимостей в go.mod)
Для сборки GUI-версии
Всё из "Для сборки Go-части" плюс:
Python 3.11+ (python.org)
PyInstaller 6.0+ (`pip install pyinstaller`)
Зависимости из `requirements.txt`:
PyQt6 ≥ 6.6
PyQt6-WebEngine ≥ 6.6 (для рендеринга веб-UI)
requests ≥ 2.28.0
psutil ≥ 5.9.0 (опционально, используется если доступен)
Для сборки установщика
Inno Setup — упаковывает всё в один `Setup.exe`, ставит приложение в `Program Files` и требует прав администратора при установке
Зависимости (автоматически включены в сборку)
wintun — TUN-адаптер для Windows (`wintun.dll`)
tun2socks — конвертер TUN→SOCKS5
xray-core — прокси-ядро (для режима sub), плюс `geoip.dat` / `geosite.dat`
helper.exe — отдельный Go-бинарь (собирается из исходников в `bin/`), поднимает мультиплексированный туннель до облачного бридж-эндпоинта и раздаёт результат как локальный SOCKS5
---
Архитектура
```
Intourist VPN/
├── cmd/
│   └── myvpn/
│       └── main.go              # Точка входа CLI/ядра (myvpn.exe)
├── internal/
│   ├── helpermgr/
│   │   └── helper.go            # Запуск helper.exe, ожидание SOCKS5, резолв IP бриджа
│   ├── tun2socksmgr/
│   │   └── tun2socks.go         # Управление tun2socks (TUN→SOCKS5)
│   ├── wintunmgr/
│   │   └── adapter.go           # Создание/закрытие Wintun-адаптера
│   ├── dnsmgr/
│   │   └── dns.go                # Настройка IP/DNS адаптера, отключение IPv6
│   ├── routemgr/
│   │   └── route.go              # Default route, bypass-маршруты, метрики
│   ├── processmgr/
│   │   └── cleanup.go            # Полная безопасная очистка при остановке
│   └── xraymgr/
│       └── xray.go               # Запуск/остановка xray.exe (используется GUI напрямую)
├── bin/                          # Исходники и бинарь helper.exe (отдельный Go-модуль)
│   ├── main.go                   # WebSocket-бридж клиент (мультиплексирование потоков)
│   ├── helper.config.yaml        # Адрес бридж-эндпоинта, токен, интервалы reconnect/ping
│   ├── helper.exe / tun2socks.exe / xray.exe / wintun.dll
│   └── geoip.dat / geosite.dat
├── intourist_vps_premium_ui/     # Веб-интерфейс GUI (HTML/CSS/JS)
│   ├── index.html
│   ├── style.css
│   └── images/
├── myvpn_gui.py                  # Точка входа GUI (PyQt6 + QWebEngineView)
├── vpn_bridge_api.py             # QWebChannel-мост между JS и Python (сигналы/слоты)
├── config_gen.py                 # Генерация xray-конфига из распарсенной ссылки/подписки
├── config.json                   # Текущий активный конфиг xray (перезаписывается при подключении)
├── build.ps1                     # Единый скрипт сборки: Go → PyInstaller → Inno Setup
├── installer.iss                 # Inno Setup скрипт установщика
├── IntouristVPN_GUI.spec         # PyInstaller спецификация (onedir)
├── myvpn_gui.manifest            # UAC-манифест
└── go.mod / go.sum                # Go-зависимости
```
Компоненты
Компонент	Язык	Назначение
`myvpn.exe`	Go	Ядро helper-режима: поднимает Wintun, запускает helper.exe и tun2socks.exe, настраивает маршруты/DNS
`helper.exe`	Go (отдельный модуль)	Устанавливает WSS-соединение с облачным бридж-эндпоинтом и мультиплексирует TCP-потоки поверх него; наружу отдаёт локальный SOCKS5 на `127.0.0.1:1080`
`tun2socks.exe`	Go	Конвертер TUN-пакетов в SOCKS5-запросы к `helper.exe`
`xray.exe`	Go	Прокси-ядро для режима sub; конфиг генерируется на лету `config_gen.py` и запускается GUI напрямую (`xray.exe run -c config.json`)
`intourist_vpn_gui.exe`	Python (PyQt6 + QWebEngine)	GUI-приложение; рендерит `intourist_vps_premium_ui/index.html`, упаковано PyInstaller (onedir)
`vpn_bridge_api.py`	Python	`QObject`, зарегистрированный в `QWebChannel` под именем `bridge` — связывает JS-интерфейс и Python-бэкенд
`config_gen.py`	Python	Парсит ссылки/подписки (VLESS, VMess, Trojan, Shadowsocks) и собирает из них конфиг xray
---
Установка и сборка
Предварительная подготовка
Клонируй репозиторий:
```powershell
   git clone <repository-url>
   cd IntouristVPN
   ```
Убедись, что PowerShell может исполнять скрипты:
```powershell
   Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
   ```
Установи зависимости Python:
```powershell
   pip install -r requirements.txt
   ```
Сборка одним скриптом
Сборка Go-бинаря, GUI и установщика теперь объединена в один скрипт:
```powershell
.\build.ps1
# или с параметрами:
.\build.ps1 -Version "2.3.0"
.\build.ps1 -SkipGo -SkipInno     # например, только пересобрать GUI без установщика
```
Что происходит:
Компилирует Go-бинарь `myvpn.exe`
Собирает GUI через PyInstaller в режиме onedir (`dist\IntouristVPN_GUI\`), используя `IntouristVPN_GUI.spec`
Копирует все зависимости (`wintun.dll`, `bin\`, `intourist_vps_premium_ui\`, `geoip.dat`, `geosite.dat`, `config.json`) в нужные места рядом с exe
Запускает Inno Setup (`installer.iss`) и собирает единый `Setup.exe`
Результат установки (в `%ProgramFiles%\Intourist VPN\`):
```
Intourist VPN\
├── intourist_vpn_gui.exe        <- PyInstaller onedir launcher
├── IntouristVPN_GUI\             <- файлы PyInstaller (включая веб-UI)
├── myvpn.exe                     <- Go-бинарь (helper-режим)
├── wintun.dll
├── xray.exe                      <- (для sub-режима)
├── geoip.dat / geosite.dat
├── config.json
├── bin\
│   ├── helper.exe
│   ├── tun2socks.exe
│   └── helper.config.yaml
├── logs\ / configs\ / temp\      <- создаются при первом запуске
└── uninstall.exe
```
> **Примечание:** Значения `authToken` и адрес бридж-эндпоинта в `bin\helper.config.yaml` специфичны для сборки/аккаунта — не публикуй их и не коммить реальные значения в открытый репозиторий.
---
Использование
CLI-режим
В этой версии `myvpn.exe` больше не принимает подкоманды `connect`/`disconnect` с адресом сервера — helper-режим теперь всегда поднимает туннель до заранее сконфигурированного бридж-эндпоинта (см. `bin\helper.config.yaml`) и работает до получения сигнала завершения:
```powershell
myvpn.exe [--base-dir <путь>]
```
Поведение:
Без аргументов — использует директорию своего exe-файла как базовую
`--base-dir <путь>` — задаёт базовую директорию явно (используется GUI при запуске `myvpn.exe` из своей папки)
Подключение поднимается автоматически при старте; для отключения — `Ctrl+C` или `SIGTERM` (процесс корректно чистит адаптер, маршруты и дочерние процессы)
Пример:
```powershell
.\myvpn.exe
# === MyVPN connected. Press Ctrl+C to disconnect. ===
# ... Ctrl+C ...
```
GUI-режим
```powershell
intourist_vpn_gui.exe
```
При запуске приложение само запрашивает повышение прав (UAC), если ещё не запущено от администратора.
Основные возможности:
Подключение к серверу:
Выбери сервер из списка (встроенный пункт "Обход белых списков" соответствует helper-режиму)
Нажми Connect
Добавление серверов/подписок:
Вставь одиночную ссылку (`vless://`, `vmess://`, `ss://`, `trojan://`) или URL подписки
Одиночная ссылка парсится сразу; подписка скачивается и разбирается в фоновом потоке
При подключении к серверу из списка используется режим `sub` (xray конфиг генерируется `config_gen.py` и пишется в `config.json`)
Проверка статуса:
Индикатор состояния подключения, счётчик трафика и времени сессии
Пинг серверов по кнопке или автоматически
Вкладка логов показывает происходящее в фоне (Python + вывод дочерних процессов)
Режим "Обход белых списков" (встроенный сервер, всегда первый в списке):
Использует `helper`-режим — туннелирует весь трафик через TUN
Системный прокси НЕ устанавливается
---
Режимы подключения
Helper (полный туннель)
Когда используется:
При подключении к встроенному серверу "Обход белых списков"
Архитектура:
```
Браузер/Приложение → маршруты Windows → TUN-адаптер wintun (MyVPN, 10.0.0.2/24)
    ↓
tun2socks.exe (TUN → SOCKS5)
    ↓
helper.exe: локальный SOCKS5 на 127.0.0.1:1080
    ↓
WSS-соединение до облачного бридж-эндпоинта (мультиплексированные TCP-потоки)
    ↓
Интернет
```
Особенности:
✅ Туннелируется весь трафик (включая DNS — устанавливается 1.1.1.1 на адаптере, IPv6 на адаптере отключается)
✅ Не требуется системный SOCKS-прокси (браузер конфигурировать не нужно)
✅ IP облачного бридж-эндпоинта резолвится и добавляется в bypass-маршруты (через реальный шлюз), чтобы сам туннель не заворачивался сам в себя
Внутренне:
`myvpn.exe` определяет реальный шлюз по умолчанию, резолвит хост бридж-эндпоинта из `helper.config.yaml`
Создаёт Wintun-адаптер `MyVPN`, настраивает IP/маску/DNS, отключает IPv6
Добавляет bypass-маршруты для IP бридж-эндпоинта через реальный шлюз (чтобы не образовалась петля)
Запускает `helper.exe`, ждёт готовности локального SOCKS5 на `127.0.0.1:1080`
Запускает `tun2socks.exe`, который читает TUN-пакеты и шлёт их через `helper.exe`
Добавляет default route через адаптер `MyVPN`
Sub (подписка / xray)
Когда используется:
При подключении к серверу, добавленному по ссылке или через подписку
Архитектура:
```
Браузер → Системные настройки прокси (socks=127.0.0.1:1080)
    ↓
xray.exe (локальный SOCKS5/HTTP-прокси на портах 1080/1081)
    ↓
Туннель к серверу (VLESS/VMess/Trojan/Shadowsocks, TLS/Reality/WS/gRPC)
    ↓
Интернет
```
Особенности:
✅ Не требует TUN-адаптера (более лёгкий, меньше конфликтов)
✅ Поддерживает VLESS, VMess, Trojan, Shadowsocks; транспорты TCP/WS/gRPC; TLS и Reality
❌ Только приложения, которые знают про SOCKS5/HTTP или используют системный прокси
❌ Нужно явно включить системный прокси (браузеры его видят через `WM_SETTINGCHANGE`)
Внутренне:
GUI парсит ссылку/подписку (Python, `config_gen.py`)
`config_gen.make_xray_config()` собирает xray-конфиг под нужный протокол/транспорт и пишет его в `config.json`
GUI напрямую запускает `xray.exe run -c config.json` (без участия `myvpn.exe`/`xraymgr`)
GUI устанавливает `ProxyServer=socks=127.0.0.1:1080` в реестр WinINet и рассылает `WM_SETTINGCHANGE`
Браузеры и другие приложения подключаются через локальный SOCKS5/HTTP
---
Разработка
Структура проекта Go
```
internal/
├── helpermgr/
│   └── helper.go        # Start/Stop helper.exe, WaitForSOCKS5, ResolveHelperIPs
├── tun2socksmgr/
│   └── tun2socks.go     # Start/Stop tun2socks
├── wintunmgr/
│   └── adapter.go       # EnsureAdapter, Close — управление Wintun
├── dnsmgr/
│   └── dns.go            # ConfigureIP, ConfigureDNS, DisableIPv6/EnableIPv6
├── routemgr/
│   └── route.go          # GetMainGateway, AddDefaultRoute, bypass-маршруты, метрики
├── processmgr/
│   └── cleanup.go        # Cleanup() — вся safe shutdown-последовательность
└── xraymgr/
    └── xray.go            # Обёртка над xray.exe (используется из Python, не из myvpn.exe)
```
Отдельно, в `bin/` лежит исходник helper.exe — это самостоятельный Go-модуль (`github.com/bridge-to-freedom/adapter`), реализующий клиент мультиплексированного WebSocket-бриджа: устанавливает соединение с облачным API-шлюзом, поддерживает reconnect с экспоненциальным backoff, периодический ping, коалессинг записи и одноразовую диагностическую пробу (`runProbe`) для проверки двунаправленного канала данных сразу после подключения.
Как работает подключение (внутреннее)
Режим helper:
`myvpn.exe` (без аргументов или с `--base-dir`) выполняет шаги `[STEP 0]`…`[STEP 7]`, залогированные в консоль/файл
`helpermgr.Start()` запускает `helper.exe`, который поднимает соединение до бридж-эндпоинта и открывает `127.0.0.1:1080`
`tun2socksmgr.Start()` запускает `tun2socks.exe`, привязывая TUN-адаптер к этому SOCKS5
При получении `SIGTERM`/`Ctrl+C` вызывается `processmgr.Cleanup()`:
Удаляет default route и bypass-маршруты, восстанавливает метрики
Останавливает `tun2socks.exe` и `helper.exe`
Закрывает Wintun-адаптер
Сбрасывает DNS адаптера
Режим sub (подписка):
GUI парсит введённую ссылку/подписку и подбирает нужный сервер
`config_gen.make_xray_config()` формирует конфиг под протокол/транспорт/security (TLS или Reality) и записывает `config.json`
GUI напрямую запускает `xray.exe run -c config.json`
GUI вызывает `_set_proxy(True)` → пишет в реестр + отправляет `WM_SETTINGCHANGE`
Браузер/приложение подключается на `127.0.0.1:1080` (или `1081` для HTTP) → xray туннелирует через сервер
Тестирование
```bash
# Синтаксис Go-кода
go vet ./...

# Сборка и запуск ядра
go build -o myvpn.exe ./cmd/myvpn
./myvpn.exe

# GUI из исходников (без сборки PyInstaller)
python myvpn_gui.py

# Проверка логов в %TEMP%/myvpn.log
type %TEMP%\myvpn.log
```
Отладка
Логи:
CLI: выводятся в консоль + пишутся в `%TEMP%/myvpn.log`
GUI: вкладка логов в веб-интерфейсе, или `%TEMP%/intourist_vpn_gui_crash.log` при необработанном исключении (перехватывается через `sys.excepthook`, процесс не падает)
---
Известные проблемы и решения
Проблема 1: Интернет перестаёт работать при подключении
Причина: Остатки от предыдущей сессии — реестр всё ещё указывает на прокси на неправильном порту, либо TUN-адаптер в плохом состоянии.
Решение:
```powershell
# 1. Убить все VPN-процессы
taskkill /IM myvpn.exe /F
taskkill /IM helper.exe /F
taskkill /IM xray.exe /F
taskkill /IM tun2socks.exe /F

# 2. Сбросить прокси
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v ProxyEnable /t REG_DWORD /d 0 /f
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v ProxyServer /t REG_SZ /d "" /f
netsh winhttp reset proxy

# 3. Сбросить маршруты
ipconfig /flushdns
route -f

# 4. Перезагрузиться (полная очистка сетевого стека)
shutdown /r /t 30
```
Проблема 2: GUI-приложение закрывается без cleanup / падает целиком из-за исключения в слоте
Причина: PyQt6 по умолчанию завершает весь процесс, если необработанное исключение вылетает внутри слота (обработчика сигнала).
Решение: Убедись, что установлен глобальный `sys.excepthook` (`_install_crash_safety_net()` в `myvpn_gui.py`) — он пишет краш в `%TEMP%\intourist_vpn_gui_crash.log` вместо падения процесса, и что `closeEvent`/`_disconnect()` обёрнуты в `try/except/finally`.
Проблема 3: Браузер не видит прокси (xray-режим)
Причина: Запись в реестр происходит, но процессы (особенно браузеры) не узнают об изменении до рестарта или перепроверки.
Решение: Убедись, что после установки прокси вызывается рассылка `WM_SETTINGCHANGE` всем окнам; при необходимости перезапусти браузер.
Проблема 4: "Осиротевший" TUN-адаптер (MyVPN)
Причина: Предыдущая сессия завершилась некорректно (например, процесс убит напрямую в диспетчере задач), и `processmgr.Cleanup()` не был вызван.
Диагностика:
```powershell
ipconfig /all
# Ищи адаптер "MyVPN" со статусом Disconnected и IP 10.0.0.x
```
Решение: Дождаться полного завершения всех процессов (`myvpn.exe`, `helper.exe`, `tun2socks.exe`) перед новым запуском; при необходимости удалить адаптер вручную через `netsh interface delete interface MyVPN`.
Проблема 5: После сборки GUI пишет «UI файл не найден»
Причина: PyInstaller (onedir) не скопировал папку `intourist_vps_premium_ui/` рядом с exe — она может оказаться внутри `_internal/` в зависимости от версии PyInstaller.
Решение:
```powershell
Copy-Item -Path "intourist_vps_premium_ui" -Destination "dist\IntouristVPN_GUI\" -Recurse -Force
```
Подробный разбор — в `TROUBLESHOOTING_UI.md`. Для разработки без сборки просто запускай `python myvpn_gui.py` из корня проекта — там UI ищется относительно исходников и находится всегда.
---
Для контрибьютеров
Если ты хочешь улучшить проект:
Форк репозитория и создай ветку для своих изменений
Внеси улучшения (новые протоколы, UI-фичи, баг-фиксы)
Протестируй на чистой Windows VM
Создай Pull Request с описанием изменений
Важные замечания для разработчиков
GUI-потокобезопасность: Все изменения в UI обязательно должны идти через сигналы `QWebChannel` (`vpn_bridge_api.py`) из главного потока Qt; фоновая работа — в `QThread` (`VpnWorker`, `SubWorker`, ping-воркеры)
Секреты: не коммить реальные значения `authToken` / адрес бридж-эндпоинта из `bin\helper.config.yaml`, а также реальные учётные данные серверов из `config.json`
Cleanup: убедись, что твой код не оставляет висящих процессов, маршрутов или реестровых записей — используй `processmgr.Cleanup()` как образец полной последовательности
Тестирование режимов: всегда тестируй оба режима (helper и sub) после изменений в `internal/` или в `config_gen.py`
Логирование: добавляй логи в соответствующий поток (Go — через `log`, Python — через `bridge.appendLog`), особенно в критичных местах cleanup'а
---
Лицензия
[Указать лицензию: MIT, GPL, Proprietary и т.д.]
---
Контакты и поддержка
Issues: Создавай issues в репозитории для баг-репортов и фичей
Discussions: Используй Discussions для вопросов и идей
---
Версия: 2.2.0
Статус: Стабильный
