package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runCommand(ctx context.Context, executable string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, executable, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", executable, args, err, string(output))
	}
	return nil
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isAgentExecutableName(name string) bool {
	lower := strings.ToLower(filepath.Base(name))
	return lower == "printmaster-agent.exe" || lower == "printmaster-agent" || lower == "pm-agent" ||
		strings.HasPrefix(lower, "printmaster-agent-v") && (strings.HasSuffix(lower, ".exe") || !strings.Contains(lower, "."))
}

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func existingPaths(paths []string) []string {
	var found []string
	for _, path := range paths {
		if path != "" && pathExists(path) {
			found = append(found, path)
		}
	}
	return found
}

func hasExecutable(installations []Installation, path string) bool {
	for _, installation := range installations {
		for _, existing := range installation.Executables {
			if strings.EqualFold(filepath.Clean(existing), filepath.Clean(path)) {
				return true
			}
		}
	}
	return false
}

func hasDataPath(installations []Installation, path string) bool {
	for _, installation := range installations {
		for _, existing := range installation.DataPaths {
			if filepath.Clean(existing) == filepath.Clean(path) {
				return true
			}
		}
	}
	return false
}

func isWithinPath(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func hasService(installations []Installation, name string) bool {
	for _, installation := range installations {
		if strings.EqualFold(installation.ServiceName, name) {
			return true
		}
	}
	return false
}

func removeRegularFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove non-regular Agent executable %q", path)
	}
	return os.Remove(path)
}

func removeTree(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to purge non-directory or linked Agent data path %q", path)
	}
	if err := rejectLinkedDescendants(path); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Clean(path))
}

func rejectLinkedDescendants(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to purge Agent data containing a symbolic link: %s", path)
		}
		return nil
	})
}
