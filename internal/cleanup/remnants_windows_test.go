//go:build windows

package cleanup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsLeftoverRecovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	data := filepath.Join(root, "PrintMaster", "agent")
	for _, path := range []string{bin, data} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	inventory := Inventory{Platform: PlatformWindows}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installations) != 2 || len(inventory.Unrecognized) != 0 {
		t.Fatalf("expected recoverable data and empty directory: %+v", inventory)
	}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionRemoveEmptyDirectory {
		t.Fatalf("data must be preserved without purge: %+v", plan)
	}
	if err := removeWindowsEmptyInstallDirectory(bin); err != nil {
		t.Fatal(err)
	}
	if pathExists(bin) || !pathExists(data) {
		t.Fatal("empty bin should be removed without deleting Agent data")
	}
	purge, err := BuildPlan(inventory, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(purge.Actions) != 2 || purge.Actions[0].Kind != ActionPurgeData {
		t.Fatalf("leftover data must be explicitly purgeable: %+v", purge)
	}
}

func TestWindowsBootstrapDirectoryPreservesUnknownFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(bin, "printmaster-server.exe")
	if err := os.WriteFile(file, []byte("shared server fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Platform: PlatformWindows}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Unrecognized) != 1 || inventory.Unrecognized[0].Path != file {
		t.Fatalf("unknown/shared files must be reported: %+v", inventory)
	}
	if err := removeWindowsEmptyInstallDirectory(bin); err == nil {
		t.Fatal("non-empty directory removal must fail")
	}
	if !pathExists(file) {
		t.Fatal("shared server file was deleted")
	}
	if err := removeWindowsEmptyInstallDirectory(filepath.Dir(bin)); err == nil {
		t.Fatal("shared PrintMaster root must never be removed")
	}
}

func TestWindowsBootstrapDirectoryRemovalFollowsExecutable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "printmaster-agent.exe")
	if err := os.WriteFile(executable, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{{
		LayoutID: "windows-bootstrap-bin", Method: MethodRawBinary, Executables: []string{executable},
	}}}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installations) != 1 {
		t.Fatal("directory cleanup must stay with its owning executable selection")
	}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 || plan.Actions[1].Kind != ActionRemoveEmptyDirectory {
		t.Fatalf("directory must be removed after the executable: %+v", plan)
	}
	if err := removeWindowsEmptyInstallDirectory(bin); err == nil {
		t.Fatal("directory must refuse removal before its executable is gone")
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if err := removeWindowsEmptyInstallDirectory(bin); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsProtectedDirectoryRecovery(t *testing.T) {
	for _, architecture := range []string{"programfiles", "programfiles-x86"} {
		t.Run(architecture, func(t *testing.T) {
			root := t.TempDir()
			programData := filepath.Join(root, "programdata")
			programFiles := filepath.Join(root, "programfiles")
			programFilesX86 := filepath.Join(root, "programfiles-x86")
			directory := filepath.Join(root, architecture, "PrintMaster", "Agent")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			allowed := windowsEmptyInstallDirectories(programData, programFiles, programFilesX86)
			inventory := Inventory{Platform: PlatformWindows}
			if err := inventoryWindowsRemnants(&inventory, programData, allowed[1:]...); err != nil {
				t.Fatal(err)
			}
			if len(inventory.Installations) != 1 || len(inventory.Unrecognized) != 0 {
				t.Fatalf("empty protected directory must be recoverable: %+v", inventory)
			}
			plan, err := BuildPlan(inventory, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionRemoveEmptyDirectory || plan.Actions[0].Target != directory {
				t.Fatalf("unexpected protected directory plan: %+v", plan)
			}
			if err := removeWindowsEmptyDirectory(filepath.Dir(directory), allowed); err == nil {
				t.Fatal("shared PrintMaster root must not be removable")
			}
			if err := removeWindowsEmptyDirectory(directory, allowed); err != nil {
				t.Fatal(err)
			}
			if pathExists(directory) || !pathExists(filepath.Dir(directory)) {
				t.Fatal("only the exact empty Agent directory should be removed")
			}
		})
	}
}

func TestWindowsProtectedDirectoryContents(t *testing.T) {
	for _, scenario := range []string{"known executable", "unknown file", "unknown directory", "msi owned"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "PrintMaster", "Agent")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			name := "printmaster-agent.exe"
			if scenario == "unknown file" || scenario == "msi owned" {
				name = "other.dll"
			}
			path := filepath.Join(directory, name)
			if scenario == "unknown directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			inventory := Inventory{Platform: PlatformWindows}
			if scenario == "known executable" || scenario == "msi owned" {
				method := MethodRawBinary
				if scenario == "msi owned" {
					method = MethodMSI
				}
				inventory.Installations = []Installation{{
					LayoutID: "windows-msi-protected", Method: method, MSIProductCode: "{fixture}",
					Executables: []string{filepath.Join(directory, "printmaster-agent.exe")},
				}}
			}
			if err := inventoryWindowsEmptyDirectory(&inventory, directory, "windows-protected-directory"); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "known executable":
				plan, err := BuildPlan(inventory, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.Actions) != 2 || plan.Actions[0].Kind != ActionRemoveExecutable || plan.Actions[1].Kind != ActionRemoveEmptyDirectory {
					t.Fatalf("directory must follow its selected executable: %+v", plan)
				}
			case "msi owned":
				if len(inventory.Unrecognized) != 0 || len(inventory.Installations[0].EmptyDirectories) != 1 {
					t.Fatal("MSI-owned directory contents must remain managed by Windows Installer")
				}
				plan, err := BuildPlan(inventory, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.Actions) != 2 || plan.Actions[0].Kind != ActionUninstallMSI || plan.Actions[1].Kind != ActionRemoveEmptyDirectory {
					t.Fatalf("leftover directory must be removed only after MSI uninstall: %+v", plan)
				}
			default:
				if len(inventory.Unrecognized) != 1 || inventory.Unrecognized[0].Path != path {
					t.Fatalf("unknown contents must block cleanup and name their path: %+v", inventory)
				}
			}
			if err := removeWindowsEmptyDirectory(directory, []string{directory}); err == nil {
				t.Fatal("non-empty protected directory must never be removed")
			}
			if !pathExists(path) {
				t.Fatal("inventory or empty-directory removal changed directory contents")
			}
		})
	}
}

func TestWindowsEmptyDirectoryRefusesLinkedPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink creation requires Windows privileges: %v", err)
	}
	directory := filepath.Join(link, "PrintMaster", "Agent")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := removeWindowsEmptyDirectory(directory, []string{directory}); err == nil {
		t.Fatal("linked installation parent must be refused")
	}
	if !pathExists(directory) {
		t.Fatal("linked target was changed")
	}
}
