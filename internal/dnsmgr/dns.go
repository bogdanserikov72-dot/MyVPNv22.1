//go:build windows

package dnsmgr

import (
	"fmt"
	"log"
	"os/exec"
)

// ConfigureDNS устанавливает DNS-сервер для указанного адаптера.
// По умолчанию используется 1.1.1.1 (Cloudflare).
func ConfigureDNS(adapterName, dnsServer string) error {
	if dnsServer == "" {
		dnsServer = "1.1.1.1"
	}
	log.Printf("[INFO] dns: setting DNS %s on '%s'", dnsServer, adapterName)
	out, err := exec.Command(
		"netsh", "interface", "ip", "set", "dns",
		"name="+adapterName,
		"static", dnsServer,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh set dns: %w\n%s", err, out)
	}
	log.Printf("[INFO] dns: DNS configured")
	return nil
}

// ConfigureIP устанавливает статический IP для адаптера.
func ConfigureIP(adapterName, ip, mask, gateway string) error {
	log.Printf("[INFO] dns: setting IP %s/%s gw %s on '%s'", ip, mask, gateway, adapterName)
	out, err := exec.Command(
		"netsh", "interface", "ip", "set", "address",
		"name="+adapterName,
		"static", ip, mask, gateway,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh set address: %w\n%s", err, out)
	}
	log.Printf("[INFO] dns: IP configured")
	return nil
}
// DisableIPv6 отключает IPv6 на всех адаптерах кроме VPN,
// чтобы браузеры не уходили через IPv6 мимо туннеля.
func DisableIPv6(vpnAdapterName string) error {
    out, err := exec.Command(
        "powershell", "-NoProfile", "-Command", `
        Get-NetAdapter |
        Where-Object { $_.Name -ne '` + vpnAdapterName + `' -and $_.Status -eq "Up" } |
        ForEach-Object { Disable-NetAdapterBinding -Name $_.Name -ComponentID ms_tcpip6 }`,
    ).CombinedOutput()
    if err != nil {
        return fmt.Errorf("disable IPv6: %w\n%s", err, out)
    }
    return nil
}
// EnableIPv6 возвращает IPv6 обратно после отключения VPN.
func EnableIPv6(vpnAdapterName string) {
    exec.Command(
        "powershell", "-NoProfile", "-Command", `
        Get-NetAdapter |
        Where-Object { $_.Name -ne '` + vpnAdapterName + `' } |
        ForEach-Object { Enable-NetAdapterBinding -Name $_.Name -ComponentID ms_tcpip6 }`,
    ).Run()
}