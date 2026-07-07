//go:build windows

package wintunmgr

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.zx2c4.com/wintun"
)

const (
	AdapterName    = "MyVPN"
	AdapterType    = "Wintun"
	AdapterIP      = "10.0.0.2"
	AdapterMask    = "255.255.255.0"
	AdapterGateway = "10.0.0.1"
)

var (
	adapter *wintun.Adapter
	// ExeDir должен быть установлен до вызова EnsureAdapter.
	// Устанавливается из main через SetExeDir.
	ExeDir string
)

// SetExeDir сохраняет директорию исполняемого файла для поиска wintun.dll.
func SetExeDir(dir string) {
	ExeDir = dir
}

// loadWintunDLL явно добавляет директорию exe в DLL search path
// чтобы golang.zx2c4.com/wintun нашёл wintun.dll.
func loadWintunDLL() error {
	if ExeDir == "" {
		return fmt.Errorf("ExeDir not set — call wintunmgr.SetExeDir() first")
	}

	dllPath := filepath.Join(ExeDir, "wintun.dll")
	log.Printf("[INFO] wintun: checking dll at: %s", dllPath)

	if _, err := os.Stat(dllPath); err != nil {
		return fmt.Errorf("wintun.dll missing at %s: %w", dllPath, err)
	}

	// SetDllDirectory добавляет директорию в начало поиска DLL.
	// Это решает проблему, когда wintun ищет DLL относительно CWD, а не EXE.
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setDllDir := kernel32.NewProc("SetDllDirectoryW")
	dirPtr, _ := syscall.UTF16PtrFromString(ExeDir)
	r, _, err := setDllDir.Call(uintptr(unsafe.Pointer(dirPtr)))
	if r == 0 {
		return fmt.Errorf("SetDllDirectoryW failed: %w", err)
	}
	log.Printf("[INFO] wintun: DLL search path set to: %s", ExeDir)
	return nil
}

// EnsureAdapter открывает существующий или создаёт новый адаптер MyVPN.
func EnsureAdapter() error {
	// Шаг 1: гарантируем, что wintun.dll будет найдена.
	if err := loadWintunDLL(); err != nil {
		return fmt.Errorf("wintun dll setup: %w", err)
	}

	var err error

	// Шаг 2: пробуем открыть существующий адаптер.
	adapter, err = wintun.OpenAdapter(AdapterName)
	if err == nil {
		log.Printf("[INFO] wintun: adapter '%s' opened (existing)", AdapterName)
		return nil
	}

	// Шаг 3: адаптер не найден — создаём новый.
	log.Printf("[INFO] wintun: creating new adapter '%s'", AdapterName)
	adapter, err = wintun.CreateAdapter(AdapterName, AdapterType, nil)
	if err != nil {
		return fmt.Errorf("wintun CreateAdapter: %w", err)
	}
	log.Printf("[INFO] wintun: adapter '%s' created", AdapterName)
	return nil
}

// Close удаляет адаптер при завершении.
func Close() {
	if adapter != nil {
		log.Printf("[INFO] wintun: closing adapter '%s'", AdapterName)
		adapter.Close()
		adapter = nil
	}
}
