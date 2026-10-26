//go:build linux

package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type linuxBackend struct{}

func NewBackend() Backend { return linuxBackend{} }

func (linuxBackend) IsElevated() bool { return os.Geteuid() == 0 }

func (linuxBackend) Scan(ctx context.Context) (Inventory, error) {
	inventory := Inventory{Platform: PlatformLinux, Installations: []Installation{}, Unrecognized: []UnrecognizedEntry{}}
	systemDataPaths := []string{"/var/lib/printmaster/agent"}
	systemConfigPaths := []string{"/etc/printmaster/agent.toml", "/etc/printmaster/config.toml", "/var/lib/printmaster/agent/config.toml"}
	systemLogPaths := []string{"/var/log/printmaster/agent", "/var/log/printmaster/agent.log"}

	if version, installed, err := queryDebPackage(ctx); err != nil {
		return Inventory{}, err
	} else if installed {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "linux-deb", Version: version, Method: MethodDeb, PackageName: "printmaster-agent",
			ServiceName: "printmaster-agent.service", ServiceScope: "system", Executables: existingPaths([]string{"/usr/bin/printmaster-agent"}),
			ConfigPaths: existingPaths(systemConfigPaths), DataPaths: existingPaths(systemDataPaths), LogPaths: existingPaths(systemLogPaths),
			Evidence: []string{"dpkg-query confirms printmaster-agent package"},
		})
	}
	if version, installed, err := queryRPM(ctx); err != nil {
		return Inventory{}, err
	} else if installed {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "linux-rpm", Version: version, Method: MethodRPM, PackageName: "printmaster-agent",
			ServiceName: "printmaster-agent.service", ServiceScope: "system", Executables: existingPaths([]string{"/usr/bin/printmaster-agent"}),
			ConfigPaths: existingPaths(systemConfigPaths), DataPaths: existingPaths(systemDataPaths), LogPaths: existingPaths(systemLogPaths),
			Evidence: []string{"rpm confirms printmaster-agent package"},
		})
	}

	knownExecutables := []string{"/usr/bin/printmaster-agent", "/usr/local/bin/printmaster-agent", "/usr/local/bin/pm-agent"}
	home, _ := os.UserHomeDir()
	userDataPath := ""
	if home != "" {
		knownExecutables = append(knownExecutables, filepath.Join(home, ".local", "bin", "printmaster-agent"))
		userDataPath = filepath.Join(home, ".local", "share", "printmaster", "agent")
	}
	for _, executable := range existingPaths(knownExecutables) {
		layoutID := "linux-direct-binary"
		if strings.HasSuffix(executable, "/pm-agent") {
			layoutID = "linux-direct-pm-agent"
		} else if strings.Contains(executable, "/.local/") {
			layoutID = "linux-user-binary"
		}
		if hasExecutable(inventory.Installations, executable) {
			continue
		}
		configPaths, dataPaths, logPaths := systemConfigPaths, systemDataPaths, systemLogPaths
		if strings.Contains(filepath.Clean(executable), string(filepath.Separator)+".local"+string(filepath.Separator)) {
			configPaths, dataPaths, logPaths = nil, []string{userDataPath}, nil
		}
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: layoutID, Method: MethodRawBinary, Executables: []string{executable},
			ConfigPaths: existingPaths(configPaths), DataPaths: existingPaths(dataPaths), LogPaths: existingPaths(logPaths),
			Evidence: []string{"known direct Agent executable path"},
		})
	}
	if userDataPath != "" && pathExists(userDataPath) && !hasDataPath(inventory.Installations, userDataPath) {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "linux-user-data", Method: MethodInteractive, DataPaths: []string{userDataPath},
			Evidence: []string{"known per-user Agent data directory"},
		})
	}

	for _, unit := range []string{"printmaster-agent.service", "PrintMasterAgent.service"} {
		loaded, err := systemdProperty(ctx, unit, "LoadState")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Inventory{}, err
		}
		if loaded != "loaded" {
			continue
		}
		execStart, err := systemdProperty(ctx, unit, "ExecStart")
		if err != nil {
			return Inventory{}, err
		}
		binary := systemdExecBinary(execStart)
		if !isKnownLinuxExecutable(binary, []string{"/usr/bin/printmaster-agent", "/usr/local/bin/printmaster-agent", "/usr/local/bin/pm-agent"}) {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "service", Name: unit, Path: binary,
				Reason: "systemd unit does not execute a known Agent binary; no action will target it",
			})
			continue
		}
		if !hasService(inventory.Installations, unit) {
			inventory.Installations = append(inventory.Installations, Installation{
				LayoutID: "linux-direct-service", Method: MethodService, ServiceName: unit, ServiceScope: "system",
				Executables: existingPaths([]string{binary}), ConfigPaths: existingPaths(systemConfigPaths),
				DataPaths: existingPaths(systemDataPaths), LogPaths: existingPaths(systemLogPaths),
				Evidence: []string{"systemd unit=" + unit, "ExecStart=" + binary},
			})
		}
	}

	processes, err := findLinuxAgentProcesses(ctx)
	if err != nil {
		return Inventory{}, err
	}
	for _, process := range processes {
		matched := false
		for index := range inventory.Installations {
			if hasExecutable([]Installation{inventory.Installations[index]}, process.Path) {
				inventory.Installations[index].Processes = append(inventory.Installations[index].Processes, process)
				matched = true
			}
		}
		if !matched {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{Kind: "process", Name: process.Name, Path: process.Path, PID: process.PID,
				Reason: "Agent-named process is not tied to a recognized installation; no action will stop it"})
		}
	}
	return inventory, nil
}

func (linuxBackend) Execute(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch action.Kind {
	case ActionRemovePackage:
		if action.Target != "printmaster-agent" {
			return fmt.Errorf("refusing unknown package %q", action.Target)
		}
		if action.Method == MethodDeb {
			if _, err := lookPath("apt-get"); err != nil {
				return fmt.Errorf("apt-get is required to remove the DEB package")
			}
			return runCommand(ctx, "apt-get", "remove", "-y", "--", action.Target)
		}
		if action.Method == MethodRPM {
			if _, err := lookPath("dnf"); err == nil {
				return runCommand(ctx, "dnf", "remove", "-y", action.Target)
			}
			if _, err := lookPath("yum"); err == nil {
				return runCommand(ctx, "yum", "remove", "-y", action.Target)
			}
			return fmt.Errorf("dnf or yum is required to remove the RPM package")
		}
		return fmt.Errorf("package removal method is missing or unsupported: %q", action.Method)
	case ActionStopService:
		return runKnownSystemctl(ctx, action.Target, "stop")
	case ActionRemoveService:
		if err := runKnownSystemctl(ctx, action.Target, "disable"); err != nil {
			return err
		}
		return removeKnownSystemdUnit(action.Target)
	case ActionStopProcess:
		return stopLinuxProcess(ctx, action.Target, action.ProcessPath)
	case ActionRemoveExecutable:
		if !isKnownLinuxExecutable(action.Target, knownLinuxExecutablePaths()) {
			return fmt.Errorf("refusing unknown executable %q", action.Target)
		}
		return removeRegularFile(action.Target)
	case ActionPurgeConfig:
		if !isKnownLinuxConfigPath(action.Target) {
			return fmt.Errorf("refusing unknown configuration path %q", action.Target)
		}
		return removeRegularFile(action.Target)
	case ActionPurgeData:
		if !isKnownLinuxDataPath(action.Target) {
			return fmt.Errorf("refusing unknown Agent data/log path %q", action.Target)
		}
		return removeTree(action.Target)
	case ActionPurgeLogs:
		if !isKnownLinuxLogPath(action.Target) {
			return fmt.Errorf("refusing unknown Agent log path %q", action.Target)
		}
		if strings.HasSuffix(action.Target, ".log") {
			return removeRegularFile(action.Target)
		}
		return removeTree(action.Target)
	default:
		return fmt.Errorf("unsupported Linux cleanup action %q", action.Kind)
	}
}

func queryDebPackage(ctx context.Context) (string, bool, error) {
	if _, err := lookPath("dpkg-query"); err != nil {
		return "", false, nil
	}
	output, err := commandOutput(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev}\t${Version}", "printmaster-agent")
	if err != nil {
		message := strings.ToLower(output + " " + err.Error())
		if strings.Contains(message, "no packages found") || strings.Contains(message, "not installed") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query installed DEB package: %w", err)
	}
	fields := strings.SplitN(strings.TrimSpace(output), "\t", 2)
	if len(fields) != 2 || !strings.Contains(fields[0], "ii") {
		return "", false, nil
	}
	return fields[1], true, nil
}

func queryRPM(ctx context.Context) (string, bool, error) {
	if _, err := lookPath("rpm"); err != nil {
		return "", false, nil
	}
	output, err := commandOutput(ctx, "rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}\n", "printmaster-agent")
	if err != nil {
		message := strings.ToLower(output + " " + err.Error())
		if strings.Contains(message, "is not installed") || strings.Contains(message, "not installed") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query installed RPM package: %w", err)
	}
	return strings.TrimSpace(output), output != "", nil
}

func systemdProperty(ctx context.Context, unit, property string) (string, error) {
	if unit != "printmaster-agent.service" && unit != "PrintMasterAgent.service" {
		return "", fmt.Errorf("refusing unknown systemd unit %q", unit)
	}
	if _, err := lookPath("systemctl"); err != nil {
		return "", os.ErrNotExist
	}
	output, err := commandOutput(ctx, "systemctl", "show", "--property="+property, "--value", "--", unit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "could not be found") {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("inspect systemd unit %s: %w", unit, err)
	}
	return strings.TrimSpace(output), nil
}

var systemdPathPattern = regexp.MustCompile(`(?:^|[;{ ]+)path=([^ ;}]+)`)

func systemdExecBinary(execStart string) string {
	match := systemdPathPattern.FindStringSubmatch(execStart)
	if len(match) == 2 {
		return match[1]
	}
	fields := strings.Fields(execStart)
	if len(fields) > 0 {
		return strings.Trim(fields[0], "{}")
	}
	return ""
}

func runKnownSystemctl(ctx context.Context, unit, verb string) error {
	if unit != "printmaster-agent.service" && unit != "PrintMasterAgent.service" {
		return fmt.Errorf("refusing unknown systemd unit %q", unit)
	}
	loaded, err := systemdProperty(ctx, unit, "LoadState")
	if errors.Is(err, os.ErrNotExist) || loaded != "loaded" && err == nil {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateKnownSystemdUnit(ctx, unit); err != nil {
		return err
	}
	return runCommand(ctx, "systemctl", verb, "--", unit)
}

func validateKnownSystemdUnit(ctx context.Context, unit string) error {
	loaded, err := systemdProperty(ctx, unit, "LoadState")
	if errors.Is(err, os.ErrNotExist) || loaded == "not-found" {
		return nil
	}
	if err != nil {
		return err
	}
	if loaded != "loaded" {
		return nil
	}
	execStart, err := systemdProperty(ctx, unit, "ExecStart")
	if err != nil {
		return err
	}
	binary := systemdExecBinary(execStart)
	if !isKnownLinuxExecutable(binary, []string{"/usr/bin/printmaster-agent", "/usr/local/bin/printmaster-agent", "/usr/local/bin/pm-agent"}) {
		return fmt.Errorf("systemd unit %s no longer executes a known Agent binary; refusing service operation", unit)
	}
	return nil
}

func removeKnownSystemdUnit(unit string) error {
	if unit != "printmaster-agent.service" && unit != "PrintMasterAgent.service" {
		return fmt.Errorf("refusing unknown systemd unit %q", unit)
	}
	if err := validateKnownSystemdUnit(context.Background(), unit); err != nil {
		return err
	}
	fragment, err := systemdProperty(context.Background(), unit, "FragmentPath")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	clean := filepath.Clean(fragment)
	if clean != filepath.Join("/etc/systemd/system", unit) && clean != filepath.Join("/usr/local/lib/systemd/system", unit) {
		return nil // package-owned unit files are removed by the package manager
	}
	if err := removeRegularFile(clean); err != nil {
		return err
	}
	return runCommand(context.Background(), "systemctl", "daemon-reload")
}

func findLinuxAgentProcesses(ctx context.Context) ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var processes []Process
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		pid64, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil || pid64 == 0 {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		commBytes, err := os.ReadFile(filepath.Join(base, "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(commBytes))
		path, err := os.Readlink(filepath.Join(base, "exe"))
		if err == nil {
			path = strings.TrimSuffix(path, " (deleted)")
		}
		if !isLinuxAgentProcess(name, path) {
			continue
		}
		processes = append(processes, Process{PID: uint32(pid64), Name: name, Path: path})
	}
	return processes, nil
}

func isLinuxAgentProcess(name, path string) bool {
	return strings.Contains(strings.ToLower(name), "printmaster-agent") ||
		isKnownLinuxExecutable(path, knownLinuxExecutablePaths())
}

func stopLinuxProcess(ctx context.Context, rawPID, expectedPath string) error {
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid Agent PID %q", rawPID)
	}
	link, err := os.Readlink(filepath.Join("/proc", rawPID, "exe"))
	if err != nil {
		return err
	}
	link = strings.TrimSuffix(link, " (deleted)")
	if expectedPath == "" || filepath.Clean(link) != filepath.Clean(expectedPath) || !isKnownLinuxExecutable(link, knownLinuxExecutablePaths()) {
		return fmt.Errorf("refusing to stop PID %d with unrecognized executable %s", pid, link)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join("/proc", rawPID)); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Agent process %d did not exit within 30 seconds", pid)
}

func knownLinuxExecutablePaths() []string {
	known := []string{"/usr/bin/printmaster-agent", "/usr/local/bin/printmaster-agent", "/usr/local/bin/pm-agent"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		known = append(known, filepath.Join(home, ".local", "bin", "printmaster-agent"))
	}
	return known
}

func isKnownLinuxExecutable(path string, known []string) bool {
	clean := filepath.Clean(path)
	for _, expected := range known {
		if clean == filepath.Clean(expected) {
			return true
		}
	}
	return false
}

func isKnownLinuxConfigPath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/etc/printmaster/agent.toml" || clean == "/etc/printmaster/config.toml" || clean == "/var/lib/printmaster/agent/config.toml"
}

func isKnownLinuxDataPath(path string) bool {
	clean := filepath.Clean(path)
	if clean == "/var/lib/printmaster/agent" {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && home != "" && clean == filepath.Join(home, ".local", "share", "printmaster", "agent")
}

func isKnownLinuxLogPath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/var/log/printmaster/agent" || clean == "/var/log/printmaster/agent.log"
}
