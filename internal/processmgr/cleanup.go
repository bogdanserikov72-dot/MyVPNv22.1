//go:build windows

package processmgr

import (
	"log"

	"myvpn/internal/dnsmgr"
	"myvpn/internal/helpermgr"
	"myvpn/internal/routemgr"
	"myvpn/internal/tun2socksmgr"
	"myvpn/internal/wintunmgr"
)

// Cleanup выполняет полную очистку в правильном порядке:
// 1. Удалить маршрут
// 2. Убить tun2socks
// 3. Убить helper
// 4. Закрыть Wintun-адаптер
func Cleanup() {
	log.Println("[INFO] cleanup: starting")

	// 1. Удалить default route
	idx := routemgr.GetStoredIfIndex()
	if idx > 0 {
		routemgr.DeleteDefaultRoute(idx)
	}
        routemgr.RestoreRouteMetrics(wintunmgr.AdapterName)
        dnsmgr.EnableIPv6(wintunmgr.AdapterName)
	routemgr.DeleteBypassRoutes()
	// 2. Остановить tun2socks
	tun2socksmgr.Stop()

	// 3. Остановить helper
	helpermgr.Stop()

	// 4. Закрыть Wintun-адаптер
	wintunmgr.Close()

	// 5. Сбросить DNS (опционально, чтобы адаптер не мусорил после удаления)
	_ = dnsmgr.ConfigureDNS(wintunmgr.AdapterName, "")

	log.Println("[INFO] cleanup: done")
}
