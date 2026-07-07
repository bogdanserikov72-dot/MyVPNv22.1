//go:build windows

package tun2socksmgr

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	DefaultDevice    = "MyVPN"
	DefaultSocksAddr = "socks5://127.0.0.1:1080"
	DefaultLogLevel  = "info"
)

var tun2socksCmd *exec.Cmd

// Start запускает tun2socks.exe с нужными параметрами.
func Start(binDir, device, socksAddr, logLevel string) error {
	if device == "" {
		device = DefaultDevice
	}
	if socksAddr == "" {
		socksAddr = DefaultSocksAddr
	}
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	t2sPath := filepath.Join(binDir, "tun2socks.exe")
	if _, err := os.Stat(t2sPath); os.IsNotExist(err) {
		return fmt.Errorf("tun2socks.exe not found at %s", t2sPath)
	}

	tun2socksCmd = exec.Command(
		t2sPath,
		"-device", device,
		"-proxy", socksAddr,
		"-loglevel", logLevel,
	)
	tun2socksCmd.Stdout = os.Stdout
	tun2socksCmd.Stderr = os.Stderr

	log.Printf("[INFO] tun2socks: starting device=%s proxy=%s", device, socksAddr)
	if err := tun2socksCmd.Start(); err != nil {
		return fmt.Errorf("start tun2socks: %w", err)
	}
	log.Printf("[INFO] tun2socks: started (pid=%d)", tun2socksCmd.Process.Pid)
	return nil
}

// Stop завершает процесс tun2socks.exe.
func Stop() {
	if tun2socksCmd != nil && tun2socksCmd.Process != nil {
		log.Printf("[INFO] tun2socks: stopping (pid=%d)", tun2socksCmd.Process.Pid)
		_ = tun2socksCmd.Process.Kill()
		_ = tun2socksCmd.Wait()
		tun2socksCmd = nil
	}
}
