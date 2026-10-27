//go:build windows

package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	agentMSIUpgradeCode = "{E1F8F20A-5D36-4DF8-879B-A868A69243AC}"
	agentMSIComponent   = "{6D585077-C1D7-40DC-87AA-02A7821C4DF1}"
	agentServiceName    = "PrintMasterAgent"
)

var (
	msiDLL                 = windows.NewLazySystemDLL("msi.dll")
	msiEnumRelatedProducts = msiDLL.NewProc("MsiEnumRelatedProductsW")
	msiGetProductInfo      = msiDLL.NewProc("MsiGetProductInfoW")
	msiGetComponentPath    = msiDLL.NewProc("MsiGetComponentPathW")
)

type windowsBackend struct{}

type windowsService struct {
	state svc.State
	path  string
}

func NewBackend() Backend { return windowsBackend{} }

func (windowsBackend) IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func (windowsBackend) Scan(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{}, Unrecognized: []UnrecognizedEntry{}}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return Inventory{}, fmt.Errorf("resolve Program Files: %w", err)
	}
	programFilesX86, _ := windows.KnownFolderPath(windows.FOLDERID_ProgramFilesX86, 0)
	localAppData, _ := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	roots := []string{
		filepath.Join(programFiles, "PrintMaster"),
		filepath.Join(programFilesX86, "PrintMaster"),
		filepath.Join(programData, "PrintMaster", "agent"),
		filepath.Join(programData, "PrintMaster", "bin"),
		filepath.Join(localAppData, "PrintMaster", "agent"),
	}

	msiProducts, err := enumerateAgentMSIProducts()
	if err != nil {
		return Inventory{}, err
	}
	for _, product := range msiProducts {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "windows-msi", Version: product.version, Method: MethodMSI,
			MSIProductCode: product.code, ServiceName: agentServiceName,
			Executables: []string{product.path},
			ConfigPaths: existingPaths([]string{
				filepath.Join(programData, "PrintMaster", "agent", "config.toml"),
				filepath.Join(programData, "PrintMaster", "config.toml"),
			}),
			DataPaths: existingPaths([]string{filepath.Join(programData, "PrintMaster", "agent")}),
			Evidence:  []string{"Windows Installer UpgradeCode " + agentMSIUpgradeCode},
		})
	}

	service, servicePath, err := inspectAgentService()
	if err != nil {
		return Inventory{}, err
	}
	if service != nil {
		layoutID := "windows-service-legacy"
		if isKnownWindowsExecutable(servicePath, roots) {
			layoutID = windowsLayoutForPath(servicePath, programData, programFiles, localAppData)
			inventory.Installations = append(inventory.Installations, Installation{
				LayoutID: layoutID, Method: MethodService, ServiceName: agentServiceName,
				Executables: existingPaths([]string{servicePath}),
				ConfigPaths: existingPaths([]string{
					filepath.Join(programData, "PrintMaster", "agent", "config.toml"),
					filepath.Join(programData, "PrintMaster", "config.toml"),
				}),
				DataPaths: existingPaths([]string{filepath.Join(programData, "PrintMaster", "agent")}),
				Evidence:  []string{fmt.Sprintf("service state=%d", service.state), "service ImagePath=" + servicePath},
			})
		} else {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "service", Name: agentServiceName, Path: servicePath,
				Reason: "service ImagePath is outside known Agent install locations; no action will target it",
			})
		}
	}

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return Inventory{}, err
		}
		if !pathExists(root) {
			continue
		}
		if strings.EqualFold(filepath.Clean(root), filepath.Clean(filepath.Join(programData, "PrintMaster", "agent"))) {
			continue
		}
		matches, err := findAgentExecutables(root)
		if err != nil {
			return Inventory{}, err
		}
		for _, executable := range matches {
			layoutID := windowsLayoutForPath(executable, programData, programFiles, localAppData)
			if hasExecutable(inventory.Installations, executable) {
				continue
			}
			dataPaths := []string{filepath.Join(programData, "PrintMaster", "agent")}
			configPaths := []string{
				filepath.Join(programData, "PrintMaster", "agent", "config.toml"),
				filepath.Join(programData, "PrintMaster", "config.toml"),
			}
			if isWithinPath(localAppData, executable) {
				dataPaths = []string{filepath.Join(localAppData, "PrintMaster", "agent")}
				configPaths = nil
			}
			inventory.Installations = append(inventory.Installations, Installation{
				LayoutID: layoutID, Method: MethodRawBinary,
				Executables: []string{executable},
				ConfigPaths: existingPaths(configPaths),
				DataPaths:   existingPaths(dataPaths),
				Evidence:    []string{"recognized executable path"},
			})
		}
	}
	userDataPath := filepath.Join(localAppData, "PrintMaster", "agent")
	if localAppData != "" && pathExists(userDataPath) && !hasDataPath(inventory.Installations, userDataPath) {
		inventory.Installations = append(inventory.Installations, Installation{
			LayoutID: "windows-user-data", Method: MethodInteractive, DataPaths: []string{userDataPath},
			Evidence: []string{"known per-user Agent data directory"},
		})
	}

	uninstallEntries, err := uninstallEntries()
	if err != nil {
		return Inventory{}, err
	}
	for _, entry := range uninstallEntries {
		if entry.KnownUpgradeCodeProduct {
			continue
		}
		if executable, matched := knownBootstrapUninstallExecutable(entry, programData); matched {
			attached := false
			for index := range inventory.Installations {
				if hasExecutable([]Installation{inventory.Installations[index]}, executable) {
					inventory.Installations[index].UninstallKey = entry.FullRegistryPath()
					inventory.Installations[index].Evidence = append(inventory.Installations[index].Evidence,
						"recognized legacy bootstrap uninstall entry="+entry.FullRegistryPath())
					attached = true
				}
			}
			if attached {
				continue
			}
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "uninstall-entry", Name: entry.DisplayName, Path: entry.FullRegistryPath(),
				Reason: "recognized bootstrap uninstall entry has no matching installed executable",
			})
			continue
		}
		inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
			Kind: "uninstall-entry", Name: entry.DisplayName, Path: entry.FullRegistryPath(),
			Reason: "Agent-named uninstall entry does not match a known MSI product or the exact legacy bootstrap identity",
		})
	}

	processes, err := findAgentProcesses()
	if err != nil {
		return Inventory{}, err
	}
	for _, process := range processes {
		matched := false
		for i := range inventory.Installations {
			if hasExecutable([]Installation{inventory.Installations[i]}, process.Path) {
				inventory.Installations[i].Processes = append(inventory.Installations[i].Processes, process)
				matched = true
			}
		}
		if !matched {
			inventory.Unrecognized = append(inventory.Unrecognized, UnrecognizedEntry{
				Kind: "process", Name: process.Name, PID: process.PID, Path: process.Path,
				Reason: "Agent-named process has an unknown or unreadable executable path; it will not be stopped",
			})
		}
	}
	return inventory, nil
}

func (windowsBackend) Execute(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch action.Kind {
	case ActionStopService:
		return stopWindowsService(ctx, action.Target)
	case ActionStopProcess:
		return stopWindowsProcess(ctx, action.Target, action.ProcessPath)
	case ActionRemoveService:
		return removeWindowsService(action.Target)
	case ActionRemoveUninstallEntry:
		return removeKnownBootstrapUninstallEntry(ctx, action.Target)
	case ActionUninstallMSI:
		return uninstallMSI(ctx, action.Target)
	case ActionRemoveExecutable:
		return removeKnownExecutable(action.Target)
	case ActionPurgeConfig:
		if !isKnownWindowsConfigPath(action.Target) {
			return fmt.Errorf("refusing unrecognized Agent configuration path %q", action.Target)
		}
		return removeRegularFile(action.Target)
	case ActionPurgeData, ActionPurgeLogs:
		return purgeKnownAgentPath(action.Target)
	default:
		return fmt.Errorf("unsupported Windows cleanup action %q", action.Kind)
	}
}

func stopWindowsProcess(ctx context.Context, rawPID, expectedPath string) error {
	var pid uint32
	if _, err := fmt.Sscanf(rawPID, "%d", &pid); err != nil || pid == 0 {
		return fmt.Errorf("invalid Agent process ID %q", rawPID)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("open Agent process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &length); err != nil {
		return fmt.Errorf("verify Agent process %d path: %w", pid, err)
	}
	path := windows.UTF16ToString(buffer[:length])
	if !strings.EqualFold(filepath.Clean(path), filepath.Clean(expectedPath)) || !isKnownWindowsExecutable(path, mustWindowsRoots()) {
		return fmt.Errorf("process %d changed to an unrecognized executable path; refusing to stop it: %s", pid, path)
	}
	if expectedPath == "" || !strings.EqualFold(filepath.Clean(path), filepath.Clean(expectedPath)) {
		return fmt.Errorf("process %d no longer matches the scanned executable; refusing to stop it", pid)
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("stop Agent process %d: %w", pid, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := windows.WaitForSingleObject(handle, 250)
		if err != nil {
			return fmt.Errorf("wait for Agent process %d: %w", pid, err)
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
	}
	return fmt.Errorf("Agent process %d did not exit within 30 seconds", pid)
}

type msiProduct struct {
	code, version, installLocation, path string
}

type windowsUninstallEntry struct {
	DisplayName             string
	RegistryHive            string
	RegistryPath            string
	ProductCode             string
	Publisher               string
	InstallLocation         string
	UninstallString         string
	QuietUninstallString    string
	WindowsInstaller        bool
	WindowsInstallerKnown   bool
	KnownUpgradeCodeProduct bool
}

func (entry windowsUninstallEntry) FullRegistryPath() string {
	return entry.RegistryHive + `\` + entry.RegistryPath
}

func enumerateAgentMSIProducts() ([]msiProduct, error) {
	upgrade, err := windows.UTF16PtrFromString(agentMSIUpgradeCode)
	if err != nil {
		return nil, err
	}
	products := make([]msiProduct, 0)
	for index := uint32(0); ; index++ {
		var code [39]uint16
		result, _, _ := msiEnumRelatedProducts.Call(uintptr(unsafe.Pointer(upgrade)), 0, uintptr(index), uintptr(unsafe.Pointer(&code[0])))
		if result == uintptr(windows.ERROR_NO_MORE_ITEMS) {
			break
		}
		if result != 0 {
			return nil, fmt.Errorf("enumerate PrintMaster Agent MSI products: %w", windows.Errno(result))
		}
		productCode := windows.UTF16ToString(code[:])
		name, err := msiProductInfo(productCode, "ProductName")
		if err != nil {
			return nil, err
		}
		if name != "PrintMaster Agent" {
			return nil, fmt.Errorf("UpgradeCode returned unexpected MSI product %q (%s)", name, productCode)
		}
		version, err := msiProductInfo(productCode, "VersionString")
		if err != nil {
			return nil, err
		}
		installLocation, _ := msiProductInfo(productCode, "InstallLocation")
		componentPtr, _ := windows.UTF16PtrFromString(agentMSIComponent)
		productPtr, _ := windows.UTF16PtrFromString(productCode)
		buffer := make([]uint16, 32768)
		length := uint32(len(buffer))
		state, _, _ := msiGetComponentPath.Call(uintptr(unsafe.Pointer(productPtr)), uintptr(unsafe.Pointer(componentPtr)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
		if int32(state) != 3 { // INSTALLSTATE_LOCAL
			return nil, fmt.Errorf("Agent MSI %s component state %d is not locally installed; inspect/repair manually", productCode, int32(state))
		}
		products = append(products, msiProduct{code: productCode, version: version, installLocation: installLocation, path: windows.UTF16ToString(buffer)})
	}
	return products, nil
}

func msiProductInfo(productCode, property string) (string, error) {
	product, err := windows.UTF16PtrFromString(productCode)
	if err != nil {
		return "", err
	}
	propertyPtr, err := windows.UTF16PtrFromString(property)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 4096)
	length := uint32(len(buffer))
	result, _, _ := msiGetProductInfo.Call(uintptr(unsafe.Pointer(product)), uintptr(unsafe.Pointer(propertyPtr)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	if result != 0 {
		return "", fmt.Errorf("read MSI property %s: %w", property, windows.Errno(result))
	}
	return windows.UTF16ToString(buffer), nil
}

func inspectAgentService() (*windowsService, string, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, "", fmt.Errorf("connect to Windows Service Control Manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(agentServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("open Agent service: %w", err)
	}
	defer service.Close()
	config, err := service.Config()
	if err != nil {
		return nil, "", fmt.Errorf("read Agent service configuration: %w", err)
	}
	executable, err := windowsServiceExecutable(config.BinaryPathName)
	if err != nil {
		return nil, "", fmt.Errorf("parse Agent service executable: %w", err)
	}
	status, err := service.Query()
	if err != nil {
		return nil, "", fmt.Errorf("read Agent service status: %w", err)
	}
	return &windowsService{state: status.State, path: executable}, executable, nil
}

func stopWindowsService(ctx context.Context, name string) error {
	if name != agentServiceName {
		return fmt.Errorf("refusing unknown service %q", name)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return err
	}
	defer service.Close()
	if err := validateWindowsService(service); err != nil {
		return err
	}
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop Agent service: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err = service.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Agent service did not stop within 30 seconds")
}

func removeWindowsService(name string) error {
	if name != agentServiceName {
		return fmt.Errorf("refusing unknown service %q", name)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return err
	}
	defer service.Close()
	if err := validateWindowsService(service); err != nil {
		return err
	}
	if err := service.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("delete Agent service registration: %w", err)
	}
	return nil
}

func validateWindowsService(service *mgr.Service) error {
	config, err := service.Config()
	if err != nil {
		return fmt.Errorf("read Agent service configuration before mutation: %w", err)
	}
	executable, err := windowsServiceExecutable(config.BinaryPathName)
	if err != nil {
		return fmt.Errorf("parse Agent service executable before mutation: %w", err)
	}
	roots, err := windowsRoots()
	if err != nil {
		return err
	}
	if !isKnownWindowsExecutable(executable, roots) {
		return fmt.Errorf("Agent service executable changed to an unknown path; refusing service mutation: %s", executable)
	}
	return nil
}

func windowsServiceExecutable(binaryPath string) (string, error) {
	arguments, err := windows.DecomposeCommandLine(binaryPath)
	if err != nil {
		return "", err
	}
	if len(arguments) == 0 || arguments[0] == "" {
		return "", fmt.Errorf("service command line has no executable")
	}
	return arguments[0], nil
}

func uninstallMSI(ctx context.Context, productCode string) error {
	products, err := enumerateAgentMSIProducts()
	if err != nil {
		return err
	}
	known := false
	for _, product := range products {
		known = known || strings.EqualFold(product.code, productCode)
	}
	if !known {
		return fmt.Errorf("MSI product %q does not belong to the PrintMaster Agent UpgradeCode", productCode)
	}
	return runCommand(ctx, `C:\Windows\System32\msiexec.exe`, "/x", productCode, "/passive", "/norestart", "REBOOT=ReallySuppress")
}

func removeKnownExecutable(path string) error {
	if !isKnownWindowsExecutable(path, mustWindowsRoots()) {
		return fmt.Errorf("refusing unrecognized executable path %q", path)
	}
	return removeRegularFile(path)
}

func purgeKnownAgentPath(path string) error {
	if !isKnownWindowsDataPath(path) {
		return fmt.Errorf("refusing unrecognized Agent data path %q", path)
	}
	return removeTree(path)
}

func findAgentExecutables(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("known Agent root is a symlink: %s", root)
	}
	if !info.IsDir() {
		if isAgentExecutableName(filepath.Base(root)) {
			return []string{root}, nil
		}
		return nil, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if isAgentExecutableName(entry.Name()) {
				return fmt.Errorf("Agent executable is a symlink: %s", path)
			}
			return filepath.SkipDir
		}
		if !entry.IsDir() && isAgentExecutableName(entry.Name()) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func uninstallEntries() ([]windowsUninstallEntry, error) {
	knownProducts := make(map[string]struct{})
	products, err := enumerateAgentMSIProducts()
	if err != nil {
		return nil, err
	}
	for _, product := range products {
		knownProducts[strings.ToUpper(product.code)] = struct{}{}
	}
	roots := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	var entries []windowsUninstallEntry
	for _, uninstallRoot := range roots {
		key, err := registry.OpenKey(uninstallRoot.root, uninstallRoot.path, registry.READ)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("open uninstall registry path %s: %w", uninstallRoot.path, err)
		}
		names, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			return nil, fmt.Errorf("enumerate uninstall registry path %s: %w", uninstallRoot.path, err)
		}
		for _, name := range names {
			entryPath := uninstallRoot.path + `\` + name
			entry, err := registry.OpenKey(uninstallRoot.root, entryPath, registry.READ)
			if err != nil {
				entries = append(entries, windowsUninstallEntry{DisplayName: "unreadable uninstall entry " + name,
					RegistryHive: registryHiveName(uninstallRoot.root), RegistryPath: entryPath})
				continue
			}
			candidate := windowsUninstallEntry{RegistryHive: registryHiveName(uninstallRoot.root), RegistryPath: entryPath}
			candidate.DisplayName = uninstallStringValue(entry, "DisplayName")
			candidate.Publisher = uninstallStringValue(entry, "Publisher")
			candidate.InstallLocation = uninstallStringValue(entry, "InstallLocation")
			candidate.UninstallString = uninstallStringValue(entry, "UninstallString")
			candidate.QuietUninstallString = uninstallStringValue(entry, "QuietUninstallString")
			windowsInstaller, _, valueErr := entry.GetIntegerValue("WindowsInstaller")
			candidate.WindowsInstallerKnown = errors.Is(valueErr, registry.ErrNotExist) || valueErr == nil
			candidate.WindowsInstaller = windowsInstaller != 0
			entry.Close()
			lower := strings.ToLower(candidate.DisplayName)
			if !strings.Contains(lower, "printmaster") || !strings.Contains(lower, "agent") {
				continue
			}
			if _, ok := knownProducts[strings.ToUpper(name)]; !ok {
				candidate.ProductCode = name
				entries = append(entries, candidate)
			}
		}
	}
	return entries, nil
}

func registryHiveName(root registry.Key) string {
	if root == registry.LOCAL_MACHINE {
		return "HKLM"
	}
	if root == registry.CURRENT_USER {
		return "HKCU"
	}
	return ""
}

func uninstallStringValue(key registry.Key, name string) string {
	value, _, _ := key.GetStringValue(name)
	return value
}

func knownBootstrapUninstallExecutable(entry windowsUninstallEntry, programData string) (string, bool) {
	const expectedKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PrintMasterAgent`
	if !strings.EqualFold(entry.RegistryHive, "HKLM") || !strings.EqualFold(entry.RegistryPath, expectedKey) {
		return "", false
	}
	if !strings.EqualFold(entry.DisplayName, "PrintMaster Agent") || !strings.EqualFold(entry.Publisher, "PrintMaster") ||
		!entry.WindowsInstallerKnown || entry.WindowsInstaller {
		return "", false
	}
	expectedDirectory := filepath.Join(programData, "PrintMaster", "bin")
	if !strings.EqualFold(filepath.Clean(entry.InstallLocation), filepath.Clean(expectedDirectory)) {
		return "", false
	}
	expectedExecutable := filepath.Join(expectedDirectory, "printmaster-agent.exe")
	if !matchesBootstrapUninstallCommand(entry.UninstallString, expectedExecutable, false) ||
		!matchesBootstrapUninstallCommand(entry.QuietUninstallString, expectedExecutable, true) {
		return "", false
	}
	return expectedExecutable, true
}

func matchesBootstrapUninstallCommand(command, expectedExecutable string, quiet bool) bool {
	arguments, err := windows.DecomposeCommandLine(command)
	if err != nil {
		return false
	}
	want := []string{expectedExecutable, "--service", "uninstall"}
	if quiet {
		want = append(want, "--silent")
	}
	if len(arguments) != len(want) || !strings.EqualFold(filepath.Clean(arguments[0]), filepath.Clean(want[0])) {
		return false
	}
	for index := 1; index < len(want); index++ {
		if !strings.EqualFold(arguments[index], want[index]) {
			return false
		}
	}
	return true
}

func removeKnownBootstrapUninstallEntry(ctx context.Context, fullPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	const expectedPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PrintMasterAgent`
	if !strings.EqualFold(fullPath, `HKLM\`+expectedPath) {
		return fmt.Errorf("refusing unrecognized uninstall registry key %q", fullPath)
	}
	entry, err := readBootstrapUninstallEntry(expectedPath)
	if err != nil {
		return err
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	if _, matched := knownBootstrapUninstallExecutable(entry, programData); !matched {
		return fmt.Errorf("legacy Agent uninstall entry changed since scan; refusing to remove %q", fullPath)
	}
	if err := registry.DeleteKey(registry.LOCAL_MACHINE, expectedPath); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove legacy Agent uninstall registry key: %w", err)
	}
	return nil
}

func readBootstrapUninstallEntry(path string) (windowsUninstallEntry, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.READ)
	if err != nil {
		return windowsUninstallEntry{}, err
	}
	defer key.Close()
	entry := windowsUninstallEntry{RegistryHive: "HKLM", RegistryPath: path}
	entry.DisplayName = uninstallStringValue(key, "DisplayName")
	entry.Publisher = uninstallStringValue(key, "Publisher")
	entry.InstallLocation = uninstallStringValue(key, "InstallLocation")
	entry.UninstallString = uninstallStringValue(key, "UninstallString")
	entry.QuietUninstallString = uninstallStringValue(key, "QuietUninstallString")
	windowsInstaller, _, valueErr := key.GetIntegerValue("WindowsInstaller")
	entry.WindowsInstallerKnown = errors.Is(valueErr, registry.ErrNotExist) || valueErr == nil
	entry.WindowsInstaller = windowsInstaller != 0
	return entry, nil
}

func mustWindowsRoots() []string {
	roots, err := windowsRoots()
	if err != nil {
		return nil
	}
	return roots
}

func windowsRoots() ([]string, error) {
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return nil, err
	}
	programFilesX86, _ := windows.KnownFolderPath(windows.FOLDERID_ProgramFilesX86, 0)
	localAppData, _ := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	roots := []string{
		filepath.Join(programFiles, "PrintMaster"),
		filepath.Join(programFilesX86, "PrintMaster"),
		filepath.Join(programData, "PrintMaster", "agent"),
		filepath.Join(programData, "PrintMaster", "bin"),
		filepath.Join(localAppData, "PrintMaster", "agent"),
	}
	return roots, nil
}

func isKnownWindowsDataPath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	if clean == strings.ToLower(filepath.Clean(filepath.Join(programData, "PrintMaster", "agent"))) {
		return true
	}
	localAppData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	return err == nil && clean == strings.ToLower(filepath.Clean(filepath.Join(localAppData, "PrintMaster", "agent")))
}

func isKnownWindowsConfigPath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return clean == strings.ToLower(filepath.Clean(filepath.Join(programData, "PrintMaster", "agent", "config.toml"))) ||
		clean == strings.ToLower(filepath.Clean(filepath.Join(programData, "PrintMaster", "config.toml")))
}

func findAgentProcesses() ([]Process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return nil, nil
		}
		return nil, err
	}
	var processes []Process
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, "printmaster-agent.exe") || strings.HasPrefix(strings.ToLower(name), "printmaster-agent-v") {
			process := Process{PID: entry.ProcessID, Name: name}
			handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
			if openErr == nil {
				buffer := make([]uint16, 32768)
				length := uint32(len(buffer))
				if queryErr := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &length); queryErr == nil {
					process.Path = windows.UTF16ToString(buffer[:length])
				}
				windows.CloseHandle(handle)
			}
			processes = append(processes, process)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	return processes, nil
}

func isKnownWindowsExecutable(path string, roots []string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		if strings.EqualFold(clean, filepath.Clean(root)) {
			return true
		}
	}
	if !isAgentExecutableName(filepath.Base(clean)) {
		return false
	}
	for _, root := range roots {
		rootInfo, err := os.Stat(root)
		if err != nil || !rootInfo.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return true
		}
	}
	return false
}

func windowsLayoutForPath(path, programData, programFiles, localAppData string) string {
	clean := strings.ToLower(filepath.Clean(path))
	switch {
	case strings.Contains(clean, strings.ToLower(filepath.Join(programFiles, "PrintMaster", "Agent"))):
		return "windows-msi-protected"
	case strings.Contains(clean, strings.ToLower(filepath.Join(programFiles, "PrintMaster"))):
		return "windows-legacy-msi-versioned"
	case strings.Contains(clean, strings.ToLower(filepath.Join(programData, "PrintMaster", "bin"))):
		return "windows-bootstrap-bin"
	case strings.Contains(clean, strings.ToLower(localAppData)):
		return "windows-user-data"
	default:
		return "windows-legacy-raw"
	}
}
