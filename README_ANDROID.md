# Intourist VPN — Android

**Intourist VPN** — Android-приложение (внутренний кодовый номер проекта — `MyVPNv22.1`) для полного туннелирования трафика через VPN. Поддерживает два режима подключения: **helper** (WSS-бридж через облачный эндпоинт) и **sub** (прокси-ядро xray с поддержкой VLESS, VMess, Trojan, Shadowsocks).

Проект состоит из двух частей:
- **Android-приложение** (`android/`) — Kotlin, `VpnService`-демон, веб-UI через `WebView`
- **Go-ядро** (`mobile/intouristcore/`) — компилируется в AAR через `gomobile bind`, весь VPN-стек собран in-process

---

## Содержание

- [Архитектура](#архитектура)
- [Требования](#требования)
- [Быстрый старт — сборка](#быстрый-старт--сборка)
  - [Автоматическая сборка через Gradle](#автоматическая-сборка-через-gradle)
  - [Ручная сборка Go-ядра](#ручная-сборка-go-ядра)
  - [Решение проблемы с пробелом в пути Windows](#решение-проблемы-с-пробелом-в-пути-windows)
- [Структура проекта](#структура-проекта)
- [Режимы подключения](#режимы-подключения)
  - [Helper (полный туннель)](#helper-полный-туннель)
  - [Sub (подписка / xray)](#sub-подписка--xray)
- [Go-ядро: intouristcore](#go-ядро-intouristcore)
- [Конфигурация](#конфигурация)
- [Известные проблемы](#известные-проблемы)

---

## Архитектура

```
Android App (Kotlin)
├── MainActivity.kt          ← WebView UI + запрос разрешений VPN
├── IntouristVpnService.kt   ← VpnService: устанавливает TUN fd, запускает Go-ядро
└── VpnBridgeApi.kt          ← JS↔Kotlin мост: парсинг подписок, генерация xray-конфига

Go-ядро (gomobile AAR)
├── intouristcore.go         ← Экспортируемый API: Start/Stop/IsRunning
├── helpermode.go            ← WSS-бридж через adapter-and-helper + in-process tun2socks
├── submode.go               ← xray-core in-process + in-process tun2socks
├── configgen.go             ← helper.config.yaml зашит в бинарник через go:embed
├── dialer.go                ← SocketProtector-обёртка для VpnService.protect()
└── logwriter.go             ← Перенаправление логов в Kotlin (LogSink)
```

### Поток данных

**Helper mode:**
```
[Трафик приложений]
      ↓
[TUN-интерфейс Android VpnService]  fd передаётся в Go
      ↓
tun2socks (in-process, xjasonlyu/tun2socks)
      ↓
SOCKS5 → 127.0.0.1:1080 (локальный listener в helpermode.go)
      ↓
adapter-and-helper: мультиплексированный WSS-туннель
      ↓
wss://...apigw.yandexcloud.net/_helper  (облачный бридж-эндпоинт)
      ↓
Интернет
```

**Sub mode:**
```
[Трафик приложений]
      ↓
[TUN-интерфейс Android VpnService]  fd передаётся в Go
      ↓
tun2socks (in-process)
      ↓
SOCKS5 → xray-core (in-process)
      ↓
VLESS / VMess / Trojan / Shadowsocks (TLS/Reality/WS/gRPC)
      ↓
Прокси-сервер → Интернет
```

---

## Требования

### Обязательно

| Инструмент | Версия | Примечание |
|---|---|---|
| Go | 1.21+ | [golang.org](https://golang.org) |
| Android SDK | compileSdk 34 | через Android Studio SDK Manager |
| Android NDK | **30.0.15729638** | обязательно именно эта версия (прошита в `build.gradle`) |
| gomobile | latest | `go install golang.org/x/mobile/cmd/gomobile@latest` |
| JDK (javac) | 11+ | Android Studio bundled JBR подходит |
| Android Studio | Iguana+ | для Kotlin-части |

### Переменные окружения (Windows)

```cmd
ANDROID_HOME=C:\Android\Sdk
ANDROID_NDK_HOME=C:\Android\Sdk\ndk\30.0.15729638
JAVA_HOME=C:\Program Files\Android\Android Studio\jbr
```

---

## Быстрый старт — сборка

### Автоматическая сборка через Gradle

Gradle-таск `bindGomobile` в `android/app/build.gradle` автоматически запускает `gomobile bind` перед каждой сборкой APK. Он сам находит NDK, javac и gomobile, обходит проблему с пробелами в пути через `X:`-диск.

```
1. Открой папку android/ в Android Studio
2. Build → Make Project
```

Таск пересобирает AAR только если исходники в `mobile/intouristcore/` новее существующего `mobile/intouristcore.aar` — повторные сборки без изменений в Go-коде быстрые.

### Ручная сборка Go-ядра

Если нужно пересобрать только AAR без полной сборки APK — открой CMD от администратора и выполни:

```cmd
subst X: "C:\Users\Halo Wolf"
set GOPATH=X:\go
set GOCACHE=X:\go\cache
set TEMP=X:\tmp
set TMP=X:\tmp
set ANDROID_HOME=C:\Android\Sdk
set ANDROID_NDK_HOME=C:\Android\Sdk\ndk\30.0.15729638
set JAVA_HOME=C:\Program Files\Android\Android Studio\jbr
set PATH=%PATH%;X:\go\bin;%JAVA_HOME%\bin
mkdir X:\tmp 2>nul

cd /d D:\MyVPNv22.1\mobile
gomobile bind -target=android -androidapi=21 -javapkg="com.intourist.gomobile" ./intouristcore
```

Результат: `D:\MyVPNv22.1\mobile\intouristcore.aar` — Gradle подхватит его автоматически при следующей сборке APK.

Для удобства сохрани эти команды как `build_aar.bat` в `D:\MyVPNv22.1\mobile\`.

### Решение проблемы с пробелом в пути Windows

Корень проблемы — NDK-тулчейн и `cgo` ломаются на путях вида `C:\Users\Halo Wolf\...`. Решение — виртуальный диск `X:` через `subst` (см. выше). `subst` сбрасывается при перезагрузке, поэтому добавь его в автозапуск или используй батник.

Альтернатива — DOS-имена (8.3):
```cmd
fsutil 8dot3name query C:   # проверить, включены ли
dir /x C:\Users             # узнать короткое имя: HALOWO~1
```

---

## Структура проекта

```
MyVPNv22.1/
├── android/                              ← Android Studio проект
│   ├── app/
│   │   ├── build.gradle                  ← bindGomobile таск, зависимости
│   │   ├── libs/                         ← сюда кладётся intouristcore.aar (auto)
│   │   └── src/main/java/com/intourist/vpn/
│   │       ├── MainActivity.kt           ← точка входа, WebView, VPN permission
│   │       ├── IntouristVpnService.kt    ← VpnService daemon
│   │       └── VpnBridgeApi.kt           ← JS↔Kotlin мост + парсинг серверов
│   └── ...
│
├── mobile/                               ← Go-модуль (gomobile bind)
│   ├── go.mod / go.sum
│   ├── intouristcore.aar                 ← генерируется gomobile (не коммитить)
│   └── intouristcore/
│       ├── intouristcore.go              ← экспортируемый API (Start/Stop/IsRunning)
│       ├── helpermode.go                 ← helper-режим: WSS-бридж + tun2socks
│       ├── submode.go                    ← sub-режим: xray-core + tun2socks
│       ├── configgen.go                  ← go:embed helper.config.yaml
│       ├── helper.config.yaml            ← конфиг бридж-эндпоинта (зашивается в AAR)
│       ├── dialer.go                     ← SocketProtector-диалер
│       └── logwriter.go                  ← перенаправление логов в Kotlin
│
└── third_party/
    └── adapter-and-helper/               ← Go-модуль WSS-бриджа (local replace)
```

### Ключевые зависимости Go (`mobile/go.mod`)

| Пакет | Версия | Назначение |
|---|---|---|
| `github.com/xtls/xray-core` | v1.8.24 | прокси-ядро для sub-режима |
| `github.com/xjasonlyu/tun2socks/v2` | v2.6.0 | TUN → SOCKS5 (оба режима) |
| `gvisor.dev/gvisor` | v0.0.0-20250523... | сетевой стек tun2socks; **пинится через `replace`** из-за конфликта с xray-core |
| `github.com/bridge-to-freedom/adapter` | local replace | WSS-бридж клиент (adapter-and-helper) |
| `gopkg.in/yaml.v3` | v3.0.1 | разбор helper.config.yaml |

> ⚠️ **Важно:** пакет `github.com/xtls/xray-core/infra/conf/serial` намеренно **не импортируется** в `submode.go` — он регистрирует wireguard-транспорт, который тянет свою копию gvisor и ломает сборку. Загрузчик JSON-конфига регистрируется вручную через специфичный минимальный набор импортов. Не добавляй `main/distro/all` и `proxy/wireguard` без необходимости.

---

## Режимы подключения

### Helper (полный туннель)

Весь трафик устройства заворачивается в TUN и уходит через облачный WSS-бридж.

- **Конфиг:** зашит в бинарник — `helper.config.yaml` встраивается через `go:embed` при компиляции AAR. Для смены бридж-эндпоинта: отредактируй `mobile/intouristcore/helper.config.yaml` и пересобери AAR.
- **Запуск:** `VpnBridgeApi.kt` → `IntouristVpnService` → `StartHelperMode(configYAML, tunFd, logSink, protector)`.
- **Готовность:** Go-сторона не сообщает Kotlin `OnStateChanged(true)` до тех пор, пока бридж реально не пройдёт аутентификацию (`upstream.HasAnyPeer()`, таймаут 20 сек). Это аналог `[STEP 5] WaitForSOCKS5` из Windows-версии — защита от "ложного connected".
- **Логирование:** в лог видны шаги `[STEP 1]`…`[STEP 4]`:
  ```
  [STEP 1] starting helper mode core
  [STEP 2] helper listener + tun2socks started
  [STEP 3] waiting for bridge to authenticate...
  [STEP 4] bridge authenticated, tunnel is live
  ```

### Sub (подписка / xray)

Трафик идёт через xray-core in-process, конфиг генерируется на лету из JSON-данных сервера.

- **Конфиг:** генерируется `makeXrayConfig()` в `VpnBridgeApi.kt` из данных сервера подписки (VLESS, VMess, Trojan, Shadowsocks; транспорты TCP/WS/gRPC; TLS/Reality). JSON передаётся строкой в `StartSubMode()`.
- **Добавление серверов:** вставь ссылку (`vless://`, `vmess://`, `ss://`, `trojan://`) или URL подписки в веб-интерфейс приложения — подписка скачивается и разбирается Kotlin-стороной.
- **Запуск:** `VpnBridgeApi.kt` → `IntouristVpnService` → `StartSubMode(xrayConfigJSON, tunFd, logSink, protector)`.

---

## Go-ядро: intouristcore

### Экспортируемый API (видим из Kotlin через gomobile)

```go
// Запустить VPN в заданном режиме
func StartHelperMode(configYAML string, tunFd int64, s LogSink, protector SocketProtector) error
func StartSubMode(xrayConfigJSON string, tunFd int64, s LogSink, protector SocketProtector) error

// Остановить VPN (безопасен для повторного вызова)
func Stop()

// Опрос состояния
func IsRunning() bool
func IsStarting() bool

// Интерфейсы, реализуемые в Kotlin
type LogSink interface {
    OnLog(msg string)
    OnError(msg string)
    OnStateChanged(connected bool, mode string)
}
type SocketProtector interface {
    Protect(fd int) bool
}
```

### Ключевые файлы и их роль

| Файл | Роль |
|---|---|
| `intouristcore.go` | Единственный exported surface; содержит `start()`, `Stop()`, `IsRunning()`, `hasBridgeURL()` |
| `helpermode.go` | `runHelperMode()` — запускает локальный SOCKS5-listener + tun2socks + WSS-сессию; `waitHelperReady()` — ждёт аутентификации бриджа |
| `submode.go` | `runSubMode()` — запускает xray-core in-process + tun2socks; содержит все нужные blank-импорты прокси-компонентов xray |
| `configgen.go` | `staticHelperConfig` (go:embed); `HelperConfigFromServerData()` (не используется в основном флоу, оставлен как утилита) |
| `dialer.go` | `newProtectedDialer()` — оборачивает `SocketProtector.Protect(fd)` для использования в Go net.Dial |
| `logwriter.go` | `logWriter` — реализует `io.Writer`, перенаправляющий стандартный лог Go в `LogSink.OnLog()` |

---

## Конфигурация

### helper.config.yaml

Файл `mobile/intouristcore/helper.config.yaml` встраивается в AAR **на этапе компиляции** через `go:embed`. На устройстве отдельного файла нет — всё внутри `.so`.

```yaml
bridge:
  url: "wss://...apigw.yandexcloud.net/_helper"
  authToken: "<токен>"
  reconnect:
    initialDelayMs: 1000
    maxDelayMs: 30000
    backoffMultiplier: 2
  pingIntervalMs: 30000

listen:
  address: "127.0.0.1:1080"

writeCoalescing:
  enabled: true
  delayMs: 50

wsApi:
  mode: "grpc"
  relay: false

logging:
  level: "info"
```

> 🔒 **Не публикуй** `authToken` и реальный URL бриджа в открытых репозиториях.

### local.properties (Android Studio)

```properties
sdk.dir=C\:\\Android\\Sdk
```

Gradle читает этот файл для поиска SDK/NDK если `ANDROID_HOME` не задан в окружении.

---

## Известные проблемы

### Конфликт gvisor (tun2socks vs xray-core wireguard)

**Симптом:** ошибка сборки вида `pkt.IsNil undefined` или `cannot use func(...) as udp.ForwarderHandler`.

**Причина:** Go MVS берёт новейшую версию gvisor из всего графа зависимостей. xray-core требует более новую версию, несовместимую с tun2socks v2.6.0.

**Решение:** в `mobile/go.mod` прописан `replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250523182742-...` — это пин версии, совместимой с tun2socks. **Не удалять.**

Параллельно в `submode.go` намеренно не импортируется `proxy/wireguard` и `main/distro/all` из xray-core — они тянут свою копию gvisor.

### sub mode: `core: Unable to load config`

**Причина:** отсутствие регистрации JSON-загрузчика xray — обычно он регистрируется через `main/distro/all` → `infra/conf/serial`, но этот импорт исключён (см. выше).

**Решение:** в `submode.go` присутствуют необходимые blank-импорты для регистрации компонентов xray (dispatcher, DNS, proxyman, router, нужные прокси). При добавлении новых протоколов в `makeXrayConfig()` — добавляй соответствующий blank-импорт в `submode.go`, но не добавляй `proxy/wireguard`.

### Пространства в пути Windows (Halo Wolf)

**Симптом:** `gomobile bind` падает с ошибкой "can't find C compiler" или "CreateProcess failed".

**Решение:** использовать `subst X: "C:\Users\Halo Wolf"` и все переменные окружения задавать через `X:\...`. Подробнее — в секции [Ручная сборка Go-ядра](#ручная-сборка-go-ядра).

### gomobile не найден

**Симптом:** `'gomobile' is not recognized as an internal or external command`.

**Причина:** `%GOPATH%\bin` не добавлен в `PATH`.

**Решение:**
```cmd
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
```
И добавь `X:\go\bin` в `PATH` (где `X:` — твой `subst`-диск).

---

## Сборка APK шпаргалка

```
Первая сборка на новом окружении:
1. subst X: "C:\Users\Halo Wolf"  (CMD от админа, при каждом перезапуске)
2. Установи переменные: ANDROID_HOME, ANDROID_NDK_HOME, JAVA_HOME (через setx или системно)
3. go install golang.org/x/mobile/cmd/gomobile@latest
4. gomobile init
5. Android Studio → android/ → Build → Make Project

Пересборка после изменений в Go-ядре:
1. subst X: "C:\Users\Halo Wolf"  (если перезагружался)
2. Запусти build_aar.bat  (или вручную gomobile bind как выше)
3. Android Studio → Build → Make Project  (подхватит новый AAR)

Пересборка только Kotlin-части:
1. Android Studio → Build → Make Project  (AAR не пересобирается если .go не менялись)
```
