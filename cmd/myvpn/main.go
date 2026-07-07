//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"myvpn/internal/dnsmgr"
	"myvpn/internal/helpermgr"
	"myvpn/internal/processmgr"
	"myvpn/internal/routemgr"
	"myvpn/internal/tun2socksmgr"
	"myvpn/internal/wintunmgr"
)

// Version может быть перезаписан ldflags при сборке.
var Version = "2.2.0"

func init() {
	// Wintun требует, чтобы поток был привязан к одному OS-потоку.
	runtime.LockOSThread()
}

func main() {
	// ── Инициализация лога ─────────────────────────────────────────────────
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stdout)

	// ── Определяем базовую директорию (exe или --base-dir) ─────────────────
	exeDir := resolveExeDir()

	// ── Начальный лог с диагностической информацией ────────────────────────
	logStartupInfo(exeDir)

	// ── Проверка всех необходимых файлов ───────────────────────────────────
	if err := checkDependencies(exeDir); err != nil {
		log.Fatalf("[FATAL] dependency check failed: %v", err)
	}

	// ── Создание необходимых директорий ────────────────────────────────────
	ensureDirectories(exeDir)

	// ── Настройка wintunmgr: передаём путь к exe, чтобы dll нашлась ────────
	wintunmgr.SetExeDir(exeDir)

	// ── binDir — директория с вспомогательными бинарями ───────────────────
	binDir := filepath.Join(exeDir, "bin")
	log.Printf("[INFO] main: exe dir=%s, bin dir=%s", exeDir, binDir)

	// ── Настройка Ctrl+C / SIGTERM ─────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[INFO] main: received signal %v, shutting down...", sig)
		processmgr.Cleanup()
		wintunmgr.Close()
		os.Exit(0)
	}()

	// ── ШАГ 0: Получить основной шлюз ДО создания Wintun (критично!) ───────
	log.Println("[STEP 0] GetMainGateway")
	mainGW, err := routemgr.GetMainGateway()
	if err != nil {
		log.Fatalf("[FATAL] cannot get main gateway: %v", err)
	}

	helperIPs, err := helpermgr.ResolveHelperIPs(binDir)
	if err != nil {
		log.Fatalf("[FATAL] cannot resolve helper IPs: %v", err)
	}

	// ── ШАГ 1: Создать/открыть Wintun-адаптер ─────────────────────────────
	log.Println("[STEP 1] EnsureAdapter")
	if err := wintunmgr.EnsureAdapter(); err != nil {
		log.Fatalf("[FATAL] wintun: %v", err)
	}

	// ── ШАГ 2: Настроить IP-адрес ──────────────────────────────────────────
	log.Println("[STEP 2] ConfigureIP")
	if err := dnsmgr.ConfigureIP(
		wintunmgr.AdapterName,
		wintunmgr.AdapterIP,
		wintunmgr.AdapterMask,
		wintunmgr.AdapterGateway,
	); err != nil {
		log.Fatalf("[FATAL] configure IP: %v", err)
	}

	// ── ШАГ 3: Настроить DNS ───────────────────────────────────────────────
	log.Println("[STEP 3] ConfigureDNS")
	if err := dnsmgr.ConfigureDNS(wintunmgr.AdapterName, "1.1.1.1"); err != nil {
		log.Printf("[WARN] configure DNS: %v (non-fatal, continuing)", err)
	}

	log.Println("[STEP 3.1] DisableIPv6")
	if err := dnsmgr.DisableIPv6(wintunmgr.AdapterName); err != nil {
		log.Printf("[WARN] disable IPv6: %v (non-fatal, continuing)", err)
	}

	// ── ШАГ 3.2: Добавить bypass-маршруты для helper/gRPC ─────────────────
	routemgr.AddBypassRoutes(helperIPs, mainGW)

	// ── ШАГ 4: Запустить helper.exe ────────────────────────────────────────
	log.Println("[STEP 4] StartHelper")
	if err := helpermgr.Start(binDir); err != nil {
		log.Fatalf("[FATAL] start helper: %v", err)
	}

	// ── ШАГ 5: Ждать SOCKS5 ────────────────────────────────────────────────
	log.Println("[STEP 5] WaitForSOCKS5 (127.0.0.1:1080)")
	if err := helpermgr.WaitForSOCKS5("127.0.0.1:1080", 30*time.Second); err != nil {
		log.Fatalf("[FATAL] wait SOCKS5: %v", err)
	}

	// ── ШАГ 6: Запустить tun2socks.exe ─────────────────────────────────────
	log.Println("[STEP 6] StartTun2Socks")
	if err := tun2socksmgr.Start(binDir, wintunmgr.AdapterName, "socks5://127.0.0.1:1080", "info"); err != nil {
		log.Fatalf("[FATAL] start tun2socks: %v", err)
	}

	// Небольшая пауза, чтобы tun2socks успел поднять стек.
	time.Sleep(2 * time.Second)

	// ── ШАГ 7: Добавить default route ──────────────────────────────────────
	log.Println("[STEP 7] AddDefaultRoute")
	idx, err := routemgr.GetIfIndex(wintunmgr.AdapterName)
	if err != nil {
		log.Printf("[WARN] get ifIndex: %v (will try route add anyway)", err)
	}
	if err := routemgr.AddDefaultRoute(idx); err != nil {
		log.Printf("[WARN] add route: %v (VPN may still work if route exists)", err)
	}
	if err := routemgr.DisableAutoMetric(wintunmgr.AdapterName); err != nil {
		log.Printf("[WARN] disable auto metric: %v (non-fatal)", err)
	}

	// ── Готово ──────────────────────────────────────────────────────────────
	log.Println("=== MyVPN connected. Press Ctrl+C to disconnect. ===")

	// Ждём сигнала завершения (обрабатывается в горутине выше).
	select {}
}

// resolveExeDir возвращает директорию исполняемого файла.
// Поддерживает флаг --base-dir для запуска из GUI.
func resolveExeDir() string {
	for i, arg := range os.Args {
		if arg == "--base-dir" && i+1 < len(os.Args) {
			dir := os.Args[i+1]
			log.Printf("[INFO] base directory from --base-dir: %s", dir)
			return dir
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("[FATAL] cannot determine exe path: %v", err)
	}

	// Разрешаем симлинки, чтобы получить реальный путь.
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}

	dir := filepath.Dir(realPath)
	log.Printf("[INFO] base directory from executable: %s", dir)
	return dir
}

// logStartupInfo выводит диагностическую информацию при запуске.
func logStartupInfo(exeDir string) {
	log.Println("=== MyVPN starting ===")
	log.Printf("[INFO] version:      %s", Version)
	log.Printf("[INFO] exe dir:      %s", exeDir)
	log.Printf("[INFO] wintun.dll:   %s", filepath.Join(exeDir, "wintun.dll"))
	log.Printf("[INFO] xray.exe:     %s", filepath.Join(exeDir, "bin", "xray.exe"))
	log.Printf("[INFO] helper.exe:   %s", filepath.Join(exeDir, "bin", "helper.exe"))
	log.Printf("[INFO] config.json:  %s", filepath.Join(exeDir, "config.json"))
	log.Printf("[INFO] geoip.dat:    %s", filepath.Join(exeDir, "geoip.dat"))
	log.Printf("[INFO] geosite.dat:  %s", filepath.Join(exeDir, "geosite.dat"))
}

// checkDependencies проверяет наличие всех необходимых файлов.
// Возвращает понятную ошибку вместо panic/fatal.
func checkDependencies(exeDir string) error {
	type dep struct {
		name string
		path string
	}

	deps := []dep{
		{"wintun.dll",   filepath.Join(exeDir, "wintun.dll")},
		{"helper.exe",   filepath.Join(exeDir, "bin", "helper.exe")},
		{"tun2socks.exe",filepath.Join(exeDir, "bin", "tun2socks.exe")},
	}

	// xray.exe и geo-файлы нужны только в режиме sub; при helper-режиме
	// они необязательны — поэтому проверяем и логируем, но не фатально.
	optDeps := []dep{
		{"xray.exe",    filepath.Join(exeDir, "bin", "xray.exe")},
		{"geoip.dat",   filepath.Join(exeDir, "geoip.dat")},
		{"geosite.dat", filepath.Join(exeDir, "geosite.dat")},
		{"config.json", filepath.Join(exeDir, "config.json")},
	}

	var missing []string
	for _, d := range deps {
		if _, err := os.Stat(d.path); err != nil {
			missing = append(missing, fmt.Sprintf("%s (expected at: %s)", d.name, d.path))
			log.Printf("[ERROR] MISSING: %s", d.path)
		} else {
			log.Printf("[INFO]  found:   %s", d.path)
		}
	}

	for _, d := range optDeps {
		if _, err := os.Stat(d.path); err != nil {
			log.Printf("[WARN]  optional missing: %s", d.path)
		} else {
			log.Printf("[INFO]  found:   %s", d.path)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("required files not found:\n  - %s\n\nPlease reinstall the application or check the installation directory.",
			joinLines(missing, "\n  - "))
	}
	return nil
}

// ensureDirectories создаёт рабочие директории при первом запуске.
func ensureDirectories(exeDir string) {
	dirs := []string{
		filepath.Join(exeDir, "logs"),
		filepath.Join(exeDir, "configs"),
		filepath.Join(exeDir, "temp"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[WARN] cannot create directory %s: %v", dir, err)
		} else {
			log.Printf("[INFO] directory ready: %s", dir)
		}
	}
}

func joinLines(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
