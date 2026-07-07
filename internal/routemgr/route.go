//go:build windows

package routemgr

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

const (
	defaultDest    = "0.0.0.0"
	defaultMask    = "0.0.0.0"
	defaultGateway = "10.0.0.1"
	routeMetric    = "1"
)

var ifIndex int

// GetIfIndex определяет индекс сетевого интерфейса по имени.
func GetIfIndex(adapterName string) (int, error) {
	out, err := exec.Command(
		"powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`(Get-NetAdapter -Name '%s' -IncludeHidden | Select-Object -First 1).ifIndex`, adapterName),
	).Output()
	if err != nil {
		return 0, fmt.Errorf("Get-NetAdapter: %w", err)
	}
	idx, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse ifIndex: %w (output: %q)", err, string(out))
	}
	log.Printf("[INFO] route: adapter '%s' ifIndex=%d", adapterName, idx)
	ifIndex = idx
	return idx, nil
}

// AddDefaultRoute добавляет маршрут по умолчанию через Wintun-адаптер.
func AddDefaultRoute(idx int) error {
	log.Printf("[INFO] route: adding default route via if %d metric %s", idx, routeMetric)
	out, err := exec.Command(
		"route", "add",
		defaultDest, "mask", defaultMask,
		defaultGateway,
		"if", strconv.Itoa(idx),
		"metric", routeMetric,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route add: %w\n%s", err, out)
	}
	log.Printf("[INFO] route: default route added")
	return nil
}

// DeleteDefaultRoute удаляет маршрут по умолчанию при cleanup.
func DeleteDefaultRoute(idx int) {
	log.Printf("[INFO] route: deleting default route via if %d", idx)
	out, err := exec.Command(
		"route", "delete",
		defaultDest, "mask", defaultMask,
		defaultGateway,
	).CombinedOutput()
	if err != nil {
		log.Printf("[WARN] route: delete failed: %v\n%s", err, out)
	} else {
		log.Printf("[INFO] route: default route deleted")
	}
}

// GetStoredIfIndex возвращает сохранённый ifIndex для cleanup.
func GetStoredIfIndex() int {
	return ifIndex
}
// DisableAutoMetricOnMainAdapters повышает метрику всех
// других default route, чтобы MyVPN имел приоритет.
func DisableAutoMetric(vpnAdapterName string) error {
	script := fmt.Sprintf(
		`Get-NetRoute -DestinationPrefix "0.0.0.0/0" | Where-Object { $_.InterfaceAlias -ne '%s' } | ForEach-Object { Set-NetRoute -InterfaceIndex $_.InterfaceIndex -DestinationPrefix "0.0.0.0/0" -RouteMetric 9999 }`,
		vpnAdapterName,
	)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set route metric: %w\n%s", err, out)
	}
	return nil
}

func RestoreRouteMetrics(vpnAdapterName string) error {
	script := fmt.Sprintf(
		`Get-NetRoute -DestinationPrefix "0.0.0.0/0" | Where-Object { $_.RouteMetric -eq 9999 } | ForEach-Object { Set-NetRoute -InterfaceIndex $_.InterfaceIndex -DestinationPrefix "0.0.0.0/0" -RouteMetric 256 }; Get-NetIPInterface | Where-Object { $_.InterfaceAlias -ne '%s' } | ForEach-Object { Set-NetIPInterface -InterfaceIndex $_.InterfaceIndex -AutomaticMetric Enabled }`,
		vpnAdapterName,
	)

	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore route metrics: %w\n%s", err, out)
	}
	return nil
}

var bypassIPs []string // сохраняем для cleanup

// AddBypassRoutes добавляет маршруты для IP helper-сервера
// через основной шлюз — ДО поднятия default route через VPN.
func AddBypassRoutes(ips []string, mainGateway string) error {
    bypassIPs = ips
    for _, ip := range ips {
        log.Printf("[INFO] route: bypass %s via %s", ip, mainGateway)
        out, err := exec.Command(
            "route", "add", ip, "mask", "255.255.255.255",
            mainGateway, "metric", "1",
        ).CombinedOutput()
        if err != nil {
            // не фатально — логируем и продолжаем
            log.Printf("[WARN] route: bypass %s failed: %v\n%s", ip, err, out)
            continue
        }
        log.Printf("[INFO] route: bypass added for %s", ip)
    }
    return nil
}

// DeleteBypassRoutes удаляет все bypass-маршруты при cleanup.
func DeleteBypassRoutes() {
    for _, ip := range bypassIPs {
        log.Printf("[INFO] route: deleting bypass for %s", ip)
        exec.Command(
            "route", "delete", ip, "mask", "255.255.255.255",
        ).Run()
    }
    bypassIPs = nil
}

// GetMainGateway возвращает шлюз по умолчанию до поднятия VPN.
func GetMainGateway() (string, error) {
    out, err := exec.Command(
        "powershell", "-NoProfile", "-Command",
        `(Get-NetRoute -DestinationPrefix "0.0.0.0/0" |
          Sort-Object RouteMetric |
          Select-Object -First 1).NextHop`,
    ).Output()
    if err != nil {
        return "", fmt.Errorf("get main gateway: %w", err)
    }
    gw := strings.TrimSpace(string(out))
    if gw == "" {
        return "", fmt.Errorf("empty gateway")
    }
    log.Printf("[INFO] route: main gateway = %s", gw)
    return gw, nil
}