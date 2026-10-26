//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32DLL    = windows.NewLazySystemDLL("shell32.dll")
	shellExecuteW = shell32DLL.NewProc("ShellExecuteW")
	user32DLL     = windows.NewLazySystemDLL("user32.dll")
	messageBoxW   = user32DLL.NewProc("MessageBoxW")
)

func ensureElevatedForGUI() (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("locate cleanup executable: %w", err)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return false, err
	}
	parameters, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(os.Args[1:]))
	if err != nil {
		return false, err
	}
	directory, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return false, err
	}
	result, _, _ := shellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(parameters)), uintptr(unsafe.Pointer(directory)), 1)
	if int32(result) <= 32 {
		if result == 1223 {
			return false, errElevationDeclined
		}
		return false, fmt.Errorf("Windows could not start the elevated cleanup process (ShellExecute error %d)", result)
	}
	return true, nil
}

func showStartupError(err error) {
	message, _ := windows.UTF16PtrFromString(err.Error())
	title, _ := windows.UTF16PtrFromString("PrintMaster Agent Cleanup")
	messageBoxW.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
