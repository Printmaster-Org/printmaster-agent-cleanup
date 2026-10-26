//go:build darwin

package cleanup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type darwinBackend struct{}

func NewBackend() Backend { return darwinBackend{} }

func (darwinBackend) IsElevated() bool { return os.Geteuid() == 0 }

func (darwinBackend) Scan(ctx context.Context) (Inventory, error) {
	inventory := Inventory{Platform: PlatformDarwin, Installations: []Installation{}, Unrecognized: []UnrecognizedEntry{}}
	home, err := os.UserHomeDir()
	if err != nil {
		return Inventory{}, err
	}
	serviceBinaryPaths := knownDarwinExecutablePaths()
	systemDataPaths := []string{"/var/lib/printmaster/agent", "/Library/Application Support/PrintMaster/agent"}
	systemConfigPaths := []string{"/etc/printmaster/agent.toml", "/etc/printmaster/config.toml"}
	systemLogPaths := []string{"/var/log/printmaster/agent", "/var/log/printmaster/agent.log"}
	userDataPath := filepath.Join(home, "Library", "Application Support", "PrintMaster", "agent")

	for _, executable := range existingPaths(append(serviceBinaryPaths, filepath.Join(home, ".local", "bin", "printmaster-agent"))) {
		if hasExecutable(inventory.Installations, executable) {
			continue
		}
		method, layout := MethodRawBinary, "darwin-direct-binary"
		if strings.Contains(executable, "/.local/") {
			layout = "darwin-user-binary"
		}
		configPaths, dataPaths, logPaths := darwinPathsForExecutable(executable, home, systemConfigPaths, systemDataPaths, systemLogPaths)
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: layout, Method: method, Executables: []string{executable},
			ConfigPaths: existingPaths(configPaths),
			DataPaths:   existingPaths(dataPaths), LogPaths: existingPaths(logPaths),
			Evidence: []string{"known macOS Agent executable"},
		})
	}

	launchPlists := []struct{ path, scope string }{
		{"/Library/LaunchDaemons/PrintMasterAgent.plist", "system"},
		{filepath.Join(home, "Library", "LaunchAgents", "PrintMasterAgent.plist"), fmt.Sprintf("gui/%d", os.Getuid())},
	}
	for _, plist := range launchPlists {
		if !pathExists(plist.path) {
			continue
		}
		program, err := launchdProgram(plist.path)
		if err != nil {
			return Inventory{}, err
		}
		if !isKnownDarwinExecutable(program, serviceBinaryPaths) {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "launchd-plist", Name: filepath.Base(plist.path), Path: plist.path,
				Reason: "launchd Program does not point to a known Agent executable; no action will target it",
			})
			continue
		}
		matched := false
		for index := range inventory.Installations {
			if hasExecutable([]Installation{inventory.Installations[index]}, program) {
				inventory.Installations[index].Method = MethodService
				inventory.Installations[index].ServiceName = "PrintMasterAgent"
				inventory.Installations[index].ServiceScope = plist.scope
				inventory.Installations[index].ServicePlist = plist.path
				inventory.Installations[index].Evidence = append(inventory.Installations[index].Evidence, "launchd plist="+plist.path)
				matched = true
			}
		}
		if !matched {
			configPaths, dataPaths, logPaths := darwinPathsForExecutable(program, home, systemConfigPaths, systemDataPaths, systemLogPaths)
			inventory.Installations = append(inventory.Installations, Installation{
				LayoutID: "darwin-launchd", Method: MethodService, ServiceName: "PrintMasterAgent", ServiceScope: plist.scope, ServicePlist: plist.path,
				Executables: existingPaths([]string{program}), ConfigPaths: existingPaths(configPaths),
				DataPaths: existingPaths(dataPaths), LogPaths: existingPaths(logPaths),
				Evidence: []string{"launchd plist=" + plist.path},
			})
		}
	}
	if pathExists(userDataPath) && !hasDataPath(inventory.Installations, userDataPath) {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "darwin-user-data", Method: MethodInteractive, DataPaths: []string{userDataPath},
			Evidence: []string{"known per-user Agent data directory"},
		})
	}

	processes, err := findDarwinAgentProcesses(ctx)
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
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "process", Name: process.Name, PID: process.PID, Path: process.Path,
				Reason: "Agent-named process is not tied to a recognized install path; no action will stop it",
			})
		}
	}
	return inventory, nil
}

func darwinPathsForExecutable(executable, home string, systemConfig, systemData, systemLogs []string) ([]string, []string, []string) {
	userExecutable := home != "" && isWithinPath(home, executable)
	if userExecutable {
		return nil, []string{filepath.Join(home, "Library", "Application Support", "PrintMaster", "agent")}, nil
	}
	return systemConfig, systemData, systemLogs
}

func (darwinBackend) Execute(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch action.Kind {
	case ActionStopService:
		if action.Target != "PrintMasterAgent" || !isKnownLaunchdPlist(action.ServiceScope, action.ServicePlist) {
			return fmt.Errorf("refusing unknown launchd service target %q/%q", action.ServiceScope, action.Target)
		}
		if err := validateLaunchdService(action.ServicePlist); err != nil {
			return err
		}
		return runCommand(ctx, "/bin/launchctl", "bootout", action.ServiceScope+"/"+action.Target)
	case ActionRemoveService:
		if action.Target != "PrintMasterAgent" || !isKnownLaunchdPlist(action.ServiceScope, action.ServicePlist) {
			return fmt.Errorf("refusing unknown launchd service/plist %q/%q", action.Target, action.ServicePlist)
		}
		if pathExists(action.ServicePlist) {
			if err := validateLaunchdService(action.ServicePlist); err != nil {
				return err
			}
			return removeRegularFile(action.ServicePlist)
		}
	case ActionStopProcess:
		return stopDarwinProcess(ctx, action.Target, action.ProcessPath)
	case ActionRemoveExecutable:
		if !isKnownDarwinExecutable(action.Target, knownDarwinExecutablePaths()) {
			return fmt.Errorf("refusing unknown executable %q", action.Target)
		}
		return removeRegularFile(action.Target)
	case ActionPurgeConfig:
		if !isKnownDarwinConfigPath(action.Target) {
			return fmt.Errorf("refusing unknown configuration %q", action.Target)
		}
		return removeRegularFile(action.Target)
	case ActionPurgeData:
		if !isKnownDarwinDataPath(action.Target) {
			return fmt.Errorf("refusing unknown Agent data path %q", action.Target)
		}
		return removeTree(action.Target)
	case ActionPurgeLogs:
		if !isKnownDarwinLogPath(action.Target) {
			return fmt.Errorf("refusing unknown Agent log path %q", action.Target)
		}
		if strings.HasSuffix(action.Target, ".log") {
			return removeRegularFile(action.Target)
		}
		return removeTree(action.Target)
	default:
		return fmt.Errorf("unsupported macOS cleanup action %q", action.Kind)
	}
	return nil
}

func validateLaunchdService(plist string) error {
	program, err := launchdProgram(plist)
	if err != nil {
		return err
	}
	if !isKnownDarwinExecutable(program, knownDarwinExecutablePaths()) {
		return fmt.Errorf("launchd plist now points to an unknown executable; refusing service mutation: %s", program)
	}
	return nil
}

func isKnownLaunchdPlist(scope, path string) bool {
	clean := filepath.Clean(path)
	if scope == "system" {
		return clean == "/Library/LaunchDaemons/PrintMasterAgent.plist"
	}
	uid, err := strconv.Atoi(strings.TrimPrefix(scope, "gui/"))
	if err != nil || scope != fmt.Sprintf("gui/%d", os.Getuid()) || uid != os.Getuid() {
		return false
	}
	home, err := os.UserHomeDir()
	return err == nil && clean == filepath.Join(home, "Library", "LaunchAgents", "PrintMasterAgent.plist")
}

func launchdProgram(path string) (string, error) {
	command := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :Program", path)
	output, err := command.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}
	command = exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :ProgramArguments:0", path)
	output, err = command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect Agent launchd plist %s: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func findDarwinAgentProcesses(ctx context.Context) ([]Process, error) {
	output, err := commandOutput(ctx, "/bin/ps", "-axo", "pid=,comm=")
	if err != nil {
		return nil, err
	}
	var processes []Process
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := filepath.Base(fields[1])
		if !isAgentExecutableName(name) {
			continue
		}
		pid, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			continue
		}
		path, _ := commandOutput(ctx, "/usr/sbin/lsof", "-a", "-p", fields[0], "-d", "txt", "-Fn")
		processPath := ""
		for _, candidate := range strings.Split(path, "\n") {
			if strings.HasPrefix(candidate, "n/") {
				processPath = strings.TrimPrefix(candidate, "n")
				break
			}
		}
		processes = append(processes, Process{PID: uint32(pid), Name: name, Path: processPath})
	}
	return processes, nil
}

func stopDarwinProcess(ctx context.Context, rawPID, expectedPath string) error {
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid Agent PID %q", rawPID)
	}
	if expectedPath == "" {
		return fmt.Errorf("missing scanned executable path for Agent PID %d", pid)
	}
	output, err := commandOutput(ctx, "/usr/sbin/lsof", "-a", "-p", rawPID, "-d", "txt", "-Fn")
	if err != nil {
		return err
	}
	matched := false
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "n/") && filepath.Clean(strings.TrimPrefix(line, "n")) == filepath.Clean(expectedPath) {
			matched = true
		}
	}
	if !matched || !isKnownDarwinExecutable(expectedPath, []string{"/usr/local/bin/printmaster-agent", "/opt/homebrew/bin/printmaster-agent"}) {
		return fmt.Errorf("process %d no longer matches the scanned executable; refusing to stop it", pid)
	}
	if err := runCommand(ctx, "/bin/kill", "-TERM", rawPID); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runCommand(ctx, "/bin/kill", "-0", rawPID); err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Agent process %d did not exit within 30 seconds", pid)
}

func isKnownDarwinExecutable(path string, known []string) bool {
	clean := filepath.Clean(path)
	for _, item := range known {
		if clean == filepath.Clean(item) {
			return true
		}
	}
	return false
}

func knownDarwinExecutablePaths() []string {
	paths := []string{"/usr/local/bin/printmaster-agent", "/opt/homebrew/bin/printmaster-agent"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".local", "bin", "printmaster-agent"))
	}
	return paths
}

func isKnownDarwinConfigPath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/etc/printmaster/agent.toml" || clean == "/etc/printmaster/config.toml"
}

func isKnownDarwinDataPath(path string) bool {
	clean := filepath.Clean(path)
	if clean == "/var/lib/printmaster/agent" || clean == "/Library/Application Support/PrintMaster/agent" {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && home != "" && clean == filepath.Join(home, "Library", "Application Support", "PrintMaster", "agent")
}

func isKnownDarwinLogPath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/var/log/printmaster/agent" || clean == "/var/log/printmaster/agent.log"
}
