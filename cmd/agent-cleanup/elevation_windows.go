//go:build windows

package main

import (
	"errors"
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
	kernel32DLL   = windows.NewLazySystemDLL("kernel32.dll")
	attachConsole = kernel32DLL.NewProc("AttachConsole")
	allocConsole  = kernel32DLL.NewProc("AllocConsole")
	getStdHandle  = kernel32DLL.NewProc("GetStdHandle")
	getFileType   = kernel32DLL.NewProc("GetFileType")
)

const (
	attachParentProcess = uintptr(0xFFFFFFFF)
	stdOutputHandle     = uintptr(0xFFFFFFF5)
	stdErrorHandle      = uintptr(0xFFFFFFF4)
	fileTypeUnknown     = 0
)

func attachConsoleForCLI() error {
	stdoutType := standardHandleType(stdOutputHandle)
	stderrType := standardHandleType(stdErrorHandle)
	if stdoutType != fileTypeUnknown && stderrType != fileTypeUnknown {
		return nil
	}
	if attached, _, _ := attachConsole.Call(attachParentProcess); attached == 0 && !errors.Is(windows.GetLastError(), windows.ERROR_ACCESS_DENIED) {
		if created, _, _ := allocConsole.Call(); created == 0 && !errors.Is(windows.GetLastError(), windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("attach CLI to a console: %w", windows.GetLastError())
		}
	}
	if stdoutType == fileTypeUnknown {
		stdout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("open console output: %w", err)
		}
		os.Stdout = stdout
	}
	if stderrType == fileTypeUnknown {
		stderr, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("open console error output: %w", err)
		}
		os.Stderr = stderr
	}
	return nil
}

func standardHandleType(handleType uintptr) uint32 {
	handle, _, _ := getStdHandle.Call(handleType)
	if handle == 0 || handle == ^uintptr(0) {
		return fileTypeUnknown
	}
	fileType, _, _ := getFileType.Call(handle)
	return uint32(fileType)
}

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
