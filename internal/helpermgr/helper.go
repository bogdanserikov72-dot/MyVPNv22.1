//go:build windows

package helpermgr

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"gopkg.in/yaml.v3"
)

var helperCmd *exec.Cmd

// Start запускает helper.exe из директории bin.
func Start(binDir string) error {
	helperPath := filepath.Join(binDir, "helper.exe")
	if _, err := os.Stat(helperPath); os.IsNotExist(err) {
		return fmt.Errorf("helper.exe not found at %s", helperPath)
	}

	helperCmd = exec.Command(helperPath)
	helperCmd.Dir = binDir
	helperCmd.Stdout = os.Stdout
	helperCmd.Stderr = os.Stderr

	log.Printf("[INFO] helper: starting %s", helperPath)
	if err := helperCmd.Start(); err != nil {
		return fmt.Errorf("start helper: %w", err)
	}
	log.Printf("[INFO] helper: started (pid=%d)", helperCmd.Process.Pid)
	return nil
}

type helperConfig struct {
    Bridge struct {
        URL string `yaml:"url"`
    } `yaml:"bridge"`
}

// WaitForSOCKS5 ждёт, пока SOCKS5-прокси станет доступен на addr.
func WaitForSOCKS5(addr string, timeout time.Duration) error {
	log.Printf("[INFO] helper: waiting for SOCKS5 at %s (timeout=%v)", addr, timeout)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			log.Printf("[INFO] helper: SOCKS5 ready at %s", addr)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for SOCKS5 at %s", addr)
}

func ResolveHelperIPs(binDir string) ([]string, error) {
    configPath := filepath.Join(binDir, "helper.config.yaml")
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, fmt.Errorf("read helper config: %w", err)
    }

    var cfg helperConfig
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("parse helper config: %w", err)
    }

    if cfg.Bridge.URL == "" {
        return nil, fmt.Errorf("bridge.url not found in config")
    }

    u, err := url.Parse(cfg.Bridge.URL)
    if err != nil {
        return nil, fmt.Errorf("parse bridge url: %w", err)
    }

    host := u.Hostname()
    log.Printf("[INFO] helper: resolving host %s", host)

    ips, err := net.LookupHost(host)
    if err != nil {
        return nil, fmt.Errorf("resolve %s: %w", host, err)
    }
    log.Printf("[INFO] helper: resolved %s → %v", host, ips)

    // ДОБАВИТЬ: резолвить второй хост (gRPC API gateway)
    grpcHost := "apigateway-connections.api.cloud.yandex.net"
    log.Printf("[INFO] helper: resolving host %s", grpcHost)
    grpcIPs, err := net.LookupHost(grpcHost)
    if err != nil {
        log.Printf("[WARN] helper: could not resolve %s: %v (continuing anyway)", grpcHost, err)
    } else {
        log.Printf("[INFO] helper: resolved %s → %v", grpcHost, grpcIPs)
        ips = append(ips, grpcIPs...)
    }

    return ips, nil
}

// Stop завершает процесс helper.exe.
func Stop() {
	if helperCmd != nil && helperCmd.Process != nil {
		log.Printf("[INFO] helper: stopping (pid=%d)", helperCmd.Process.Pid)
		_ = helperCmd.Process.Kill()
		_ = helperCmd.Wait()
		helperCmd = nil
	}
}
