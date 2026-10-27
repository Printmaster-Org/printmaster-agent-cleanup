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

func inventoryWindowsRemnants(inventory *Inventory, programData string) error {
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

	bin := filepath.Join(programData, "PrintMaster", "bin")
	info, err = os.Lstat(bin)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect bootstrap directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bootstrap path is not a regular directory: %s", bin)
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		return fmt.Errorf("read bootstrap directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(bin, entry.Name())
		if entry.IsDir() || !hasExecutable(inventory.Installations, path) {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "bootstrap-remnant", Name: entry.Name(), Path: path,
				Reason: "bootstrap directory contains an unrecognized or shared entry; it will not be deleted and may block a fresh bootstrap",
			})
			return nil
		}
	}
	for index := range inventory.Installations {
		for _, executable := range inventory.Installations[index].Executables {
			if strings.EqualFold(filepath.Dir(executable), bin) {
				inventory.Installations[index].EmptyDirectories = []string{bin}
				return nil
			}
		}
	}
	inventory.Installations = append(inventory.Installations, Installation{
		LayoutID: "windows-bootstrap-directory", Method: MethodInteractive,
		EmptyDirectories: []string{bin},
		Evidence:         []string{"known bootstrap directory; remove only after selected executables are gone and the directory is empty"},
	})
	return nil
}

func removeWindowsBootstrapDirectory(path string) error {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	if !strings.EqualFold(filepath.Clean(path), filepath.Join(programData, "PrintMaster", "bin")) {
		return fmt.Errorf("refusing unrecognized bootstrap directory %q", path)
	}
	for _, directory := range []string{programData, filepath.Join(programData, "PrintMaster"), path} {
		attributes, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(directory))
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect bootstrap directory %s: %w", directory, err)
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fmt.Errorf("refusing linked or non-directory bootstrap path %q", directory)
		}
	}
	// os.Remove is deliberately non-recursive and refuses a directory with any remaining files.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove empty bootstrap directory (remaining files are preserved): %w", err)
	}
	return nil
}
