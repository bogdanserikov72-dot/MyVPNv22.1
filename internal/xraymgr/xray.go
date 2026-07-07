package xraymgr

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Manager управляет процессом xray.exe.
type Manager struct {
	cmd    *exec.Cmd
	ExeDir string // директория MyVPN.exe; должна быть задана до Start()
}

// Start запускает xray.exe run -config <config> из директории ExeDir.
// Это устраняет ошибку "bin/xray.exe not found" при запуске из произвольного CWD.
func (m *Manager) Start(config string, stdout io.Writer) error {
	if m.ExeDir == "" {
		return fmt.Errorf("xraymgr: ExeDir not set")
	}

	xrayPath := filepath.Join(m.ExeDir, "xray.exe")
	if _, err := os.Stat(xrayPath); err != nil {
		return fmt.Errorf("xray.exe not found at %s: %w", xrayPath, err)
	}

	m.cmd = exec.Command(xrayPath, "run", "-config", config)
	m.cmd.Dir = m.ExeDir // рабочая директория = папка с EXE
	m.cmd.Stdout = stdout
	m.cmd.Stderr = stdout

	return m.cmd.Start()
}

// Stop завершает процесс xray.exe.
func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}
}
