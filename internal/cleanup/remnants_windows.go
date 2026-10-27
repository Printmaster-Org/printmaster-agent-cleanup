//go:build windows

package cleanup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func inventoryWindowsRemnants(inventory *Inventory, programData string, protectedRoots ...string) error {
	data := filepath.Join(programData, "PrintMaster", "agent")
	info, err := os.Lstat(data)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect leftover Agent data: %w", err)
	}
	if err == nil && !hasDataPath(inventory.Installations, data) {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("leftover Agent data is not a regular directory: %s", data)
		}
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "windows-machine-data", Method: MethodInteractive,
			DataPaths: []string{data},
			ConfigPaths: existingPaths([]string{
				filepath.Join(data, "config.toml"), filepath.Join(programData, "PrintMaster", "config.toml"),
			}),
			Evidence: []string{"leftover machine-wide Agent data; preserved unless purge is selected"},
		})
	}

	if err := inventoryWindowsEmptyDirectory(inventory, filepath.Join(programData, "PrintMaster", "bin"), "windows-bootstrap-directory"); err != nil {
		return err
	}
	for _, root := range protectedRoots {
		if err := inventoryWindowsEmptyDirectory(inventory, root, "windows-protected-directory"); err != nil {
			return err
		}
	}
	return nil
}

func inventoryWindowsEmptyDirectory(inventory *Inventory, directory, layoutID string) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Agent installation directory %s: %w", directory, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Agent installation path is not a regular directory: %s", directory)
	}
	// Windows Installer owns package contents; only remove a leftover directory after uninstall.
	for index, installation := range inventory.Installations {
		if installation.Method == MethodMSI {
			for _, executable := range installation.Executables {
				if isWithinPath(directory, executable) {
					inventory.Installations[index].EmptyDirectories = append(inventory.Installations[index].EmptyDirectories, directory)
					return nil
				}
			}
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read Agent installation directory %s: %w", directory, err)
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() || !hasExecutable(inventory.Installations, path) {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "bootstrap-remnant", Name: entry.Name(), Path: path,
				Reason: "Agent installation directory contains an unrecognized or shared entry; it will not be deleted and may block a fresh bootstrap",
			})
			return nil
		}
	}
	for index := range inventory.Installations {
		for _, executable := range inventory.Installations[index].Executables {
			if strings.EqualFold(filepath.Dir(executable), directory) {
				inventory.Installations[index].EmptyDirectories = append(inventory.Installations[index].EmptyDirectories, directory)
				return nil
			}
		}
	}
	inventory.Installations = append(inventory.Installations, Installation{
		LayoutID: layoutID, Method: MethodInteractive,
		EmptyDirectories: []string{directory},
		Evidence:         []string{"known Agent installation directory blocks fresh bootstrap even when empty; remove only when empty"},
	})
	return nil
}

func removeWindowsEmptyInstallDirectory(path string) error {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return fmt.Errorf("resolve Program Files for directory removal: %w", err)
	}
	programFilesX86, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFilesX86, 0)
	if err != nil {
		return fmt.Errorf("resolve Program Files (x86) for directory removal: %w", err)
	}
	return removeWindowsEmptyDirectory(path, windowsEmptyInstallDirectories(programData, programFiles, programFilesX86))
}

func windowsEmptyInstallDirectories(programData, programFiles, programFilesX86 string) []string {
	directories := []string{filepath.Join(programData, "PrintMaster", "bin")}
	for _, base := range []string{programFiles, programFilesX86} {
		if base != "" {
			directories = append(directories, filepath.Join(base, "PrintMaster", "Agent"))
		}
	}
	return directories
}

func removeWindowsEmptyDirectory(path string, allowed []string) error {
	matched := false
	for _, directory := range allowed {
		matched = matched || strings.EqualFold(filepath.Clean(path), filepath.Clean(directory))
	}
	if !matched {
		return fmt.Errorf("refusing unrecognized Agent installation directory %q", path)
	}
	for _, directory := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path), path} {
		attributes, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(directory))
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect Agent installation directory %s: %w", directory, err)
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fmt.Errorf("refusing linked or non-directory Agent installation path %q", directory)
		}
	}
	// os.Remove is deliberately non-recursive and refuses a directory with any remaining files.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove empty Agent installation directory (remaining files are preserved): %w", err)
	}
	return nil
}
